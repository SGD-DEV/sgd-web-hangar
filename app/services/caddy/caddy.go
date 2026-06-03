// Package caddy wraps the Caddy web server as a Hangar service. Caddy
// is an alternative to Apache and Nginx: auto-HTTPS via Let's
// Encrypt (or local mkcert), tiny Caddyfile config, single-binary.
//
// Port conflicts with Apache/Nginx: Caddy defaults to 80 and 443,
// the same ports Apache/Nginx listen on. Hangar registers Caddy as a
// service so users can choose it, but the Servers page should not
// auto-start Caddy if Apache or Nginx is already running. Start()
// here does not enforce that - it's the caller's job (the manager or
// the UI) to decide.
//
// The default Caddyfile we emit on first start listens on 8090 for
// HTTP so it can co-exist with Apache on 80 for the "I want to try
// Caddy alongside Apache" workflow. Users who replace Apache with
// Caddy can edit the Caddyfile manually.
package caddy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/logs"
	"github.com/devour-app/devour/app/services"
)

const defaultCaddyfile = `# Hangar default Caddyfile - lives at data/conf/caddy/Caddyfile.
#
# Caddy listens on 8090 here so it does not collide with Apache (80)
# or Nginx (often 8080). Replace this block with your real site config
# or symlink your project's Caddyfile. Hangar will reload Caddy when
# you click Restart in the Servers page.
:8090 {
	respond "Hangar default site - replace data/conf/caddy/Caddyfile with your config"
}
`

type Caddy struct {
	services.BaseService
	paths    config.Paths
	store    *config.Store
	logStore *logs.Store
	cmd      *exec.Cmd
	mu       sync.Mutex
	version  string
}

func New(paths config.Paths, store *config.Store, logStore *logs.Store) *Caddy {
	return &Caddy{
		BaseService: services.BaseService{
			ServiceName: "caddy",
			ServicePort: 8090,
		},
		paths:    paths,
		store:    store,
		logStore: logStore,
	}
}

func (c *Caddy) Name() string { return "caddy" }

func (c *Caddy) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Running {
		return nil
	}
	c.StatusText = services.StatusStarting
	c.LastError = ""

	binPath, err := c.findBinary()
	if err != nil {
		c.StatusText = services.StatusStopped
		return fmt.Errorf("caddy: %w", err)
	}

	caddyfile, err := c.ensureCaddyfile()
	if err != nil {
		c.StatusText = services.StatusStopped
		return fmt.Errorf("caddy: ensuring Caddyfile: %w", err)
	}

	c.cmd = exec.Command(binPath,
		"run",
		"--config", caddyfile,
		"--adapter", "caddyfile",
	)
	c.cmd.Dir = filepath.Dir(binPath)
	services.HideWindow(c.cmd)

	if err := c.cmd.Start(); err != nil {
		c.StatusText = services.StatusStopped
		c.LastError = err.Error()
		return fmt.Errorf("caddy: starting: %w", err)
	}

	c.Running = true
	c.StatusText = services.StatusRunning
	c.ProcessPID = c.cmd.Process.Pid
	c.StartTime = time.Now()
	go c.waitForExit()

	c.logStore.Add("caddy", fmt.Sprintf("Caddy started (PID: %d) — Caddyfile %s", c.ProcessPID, caddyfile))
	return nil
}

func (c *Caddy) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.Running || c.cmd == nil || c.cmd.Process == nil {
		c.Running = false
		c.StatusText = ""
		return nil
	}
	c.StatusText = services.StatusStopping
	pid := c.cmd.Process.Pid

	// Caddy's "stop" command would be cleaner but requires the admin
	// API to be reachable. Killing the process is simpler and matches
	// what we do for the other services.
	_ = c.cmd.Process.Kill()

	done := make(chan struct{})
	go func() { _ = c.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		c.logStore.Add("caddy", "Caddy did not stop in time, force killed")
	}

	c.Running = false
	c.StatusText = ""
	c.ProcessPID = 0
	c.StartTime = time.Time{}
	c.cmd = nil
	c.logStore.Add("caddy", fmt.Sprintf("Caddy stopped (was PID: %d)", pid))
	return nil
}

func (c *Caddy) Restart() error {
	if err := c.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return c.Start()
}

func (c *Caddy) Status() services.ServiceStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	status := services.StatusStopped
	if c.StatusText != "" {
		status = c.StatusText
	} else if c.Running {
		status = services.StatusRunning
	}

	startedAt := ""
	if !c.StartTime.IsZero() {
		startedAt = c.StartTime.Format(time.RFC3339)
	}

	return services.ServiceStatus{
		Name:      "caddy",
		Status:    status,
		Port:      c.ServicePort,
		Version:   c.version,
		PID:       c.ProcessPID,
		Uptime:    c.GetUptime(),
		StartedAt: startedAt,
		Error:     c.LastError,
	}
}

func (c *Caddy) Logs(lines int) []string { return c.logStore.Get("caddy", lines) }

func (c *Caddy) Version() string { return c.version }

func (c *Caddy) SetVersion(version string) error {
	installPath := filepath.Join(c.paths.InstalledPath(), "caddy", version)
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("caddy: version %s not installed", version)
	}
	c.version = version
	return c.store.SaveServiceConfig("caddy", config.ServiceConfig{
		Name:        "caddy",
		Version:     version,
		InstallPath: installPath,
		Port:        c.ServicePort,
		Enabled:     true,
	})
}

func (c *Caddy) IsInstalled() bool {
	_, err := c.findBinary()
	return err == nil
}

// EnsureConfig pre-generates the default Caddyfile so the config
// editor doesn't show "file not found" on first open. Symmetrical
// with MySQL/PostgreSQL's EnsureConfig hook in core's
// auto-config-create step.
func (c *Caddy) EnsureConfig() error {
	_, err := c.ensureCaddyfile()
	return err
}

// findBinary handles the Caddy zip layout, which is flat:
// installed/caddy/<version>/caddy.exe (no nested dir, no bin/).
func (c *Caddy) findBinary() (string, error) {
	if cfg, err := c.store.GetServiceConfig("caddy"); err == nil && cfg.InstallPath != "" {
		bin := filepath.Join(cfg.InstallPath, "caddy.exe")
		if _, err := os.Stat(bin); err == nil {
			c.version = cfg.Version
			return bin, nil
		}
	}
	installedDir := filepath.Join(c.paths.InstalledPath(), "caddy")
	entries, err := os.ReadDir(installedDir)
	if err != nil {
		return "", fmt.Errorf("no caddy installation found - install it from the Packages page")
	}
	for _, e := range entries {
		if e.IsDir() {
			bin := filepath.Join(installedDir, e.Name(), "caddy.exe")
			if _, err := os.Stat(bin); err == nil {
				c.version = e.Name()
				return bin, nil
			}
		}
	}
	return "", fmt.Errorf("no caddy installation found - install it from the Packages page")
}

func (c *Caddy) ensureCaddyfile() (string, error) {
	confDir := c.paths.ConfPath("caddy")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return "", err
	}
	caddyfile := filepath.Join(confDir, "Caddyfile")
	if _, err := os.Stat(caddyfile); os.IsNotExist(err) {
		if err := os.WriteFile(caddyfile, []byte(defaultCaddyfile), 0o644); err != nil {
			return "", err
		}
		c.logStore.Add("caddy", "Generated default Caddyfile at "+caddyfile)
	}
	return caddyfile, nil
}

func (c *Caddy) waitForExit() {
	if c.cmd != nil {
		_ = c.cmd.Wait()
	}
	c.mu.Lock()
	if c.Running {
		c.Running = false
		c.StatusText = ""
		c.ProcessPID = 0
		c.logStore.Add("caddy", "Caddy process exited")
	}
	c.mu.Unlock()
}
