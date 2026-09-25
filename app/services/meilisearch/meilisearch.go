// Package meilisearch wraps Meilisearch (search engine) as a Hangar
// service. Meilisearch ships as a single self-contained .exe; we run
// it with --db-path under %LOCALAPPDATA%\Hangar\data\meilisearch and a
// developer-only master key. For production deployments users should
// set their own key - the bundled default is suitable only for local
// dev. Default HTTP API on :7700.
package meilisearch

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

// devMasterKey is what Meilisearch refuses to start without on the
// HTTP API in current versions. We use a stable dev key so the user's
// local Meilisearch clients don't break across restarts. It is
// LOCAL-DEV ONLY - production deployments must override via the
// Settings page (TODO) or env file.
const devMasterKey = "hangar-local-dev-master-key-not-for-production"

// MasterKey is the key the Meilisearch web UI and admin clients log in with.
func MasterKey() string { return devMasterKey }

type Meilisearch struct {
	services.BaseService
	paths    config.Paths
	store    *config.Store
	logStore *logs.Store
	cmd      *exec.Cmd
	mu       sync.Mutex
	version  string
}

func New(paths config.Paths, store *config.Store, logStore *logs.Store) *Meilisearch {
	return &Meilisearch{
		BaseService: services.BaseService{
			ServiceName: "meilisearch",
			ServicePort: 7700,
		},
		paths:    paths,
		store:    store,
		logStore: logStore,
	}
}

func (m *Meilisearch) Name() string { return "meilisearch" }

func (m *Meilisearch) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Running {
		return nil
	}
	m.StatusText = services.StatusStarting
	m.LastError = ""

	binPath, err := m.findBinary()
	if err != nil {
		m.StatusText = services.StatusStopped
		return fmt.Errorf("meilisearch: %w", err)
	}

	dbPath := filepath.Join(m.paths.DataPath(), "meilisearch")
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		m.StatusText = services.StatusStopped
		return fmt.Errorf("meilisearch: creating data dir: %w", err)
	}

	m.cmd = exec.Command(binPath,
		"--db-path", dbPath,
		"--http-addr", fmt.Sprintf("127.0.0.1:%d", m.ServicePort),
		"--master-key", devMasterKey,
		"--no-analytics", // we do not phone home from a local dev tool
		"--env", "development",
	)
	m.cmd.Dir = filepath.Dir(binPath)
	services.HideWindow(m.cmd)

	if err := m.cmd.Start(); err != nil {
		m.StatusText = services.StatusStopped
		m.LastError = err.Error()
		return fmt.Errorf("meilisearch: starting: %w", err)
	}

	m.Running = true
	m.StatusText = services.StatusRunning
	m.ProcessPID = m.cmd.Process.Pid
	m.StartTime = time.Now()
	go m.waitForExit()

	m.logStore.Add("meilisearch", fmt.Sprintf("Meilisearch started (PID: %d) — HTTP :%d, db-path %s", m.ProcessPID, m.ServicePort, dbPath))
	return nil
}

func (m *Meilisearch) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.Running || m.cmd == nil || m.cmd.Process == nil {
		m.Running = false
		m.StatusText = ""
		return nil
	}
	m.StatusText = services.StatusStopping
	pid := m.cmd.Process.Pid
	_ = m.cmd.Process.Kill()

	done := make(chan struct{})
	go func() { _ = m.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		m.logStore.Add("meilisearch", "Meilisearch did not stop in time, force killed")
	}

	m.Running = false
	m.StatusText = ""
	m.ProcessPID = 0
	m.StartTime = time.Time{}
	m.cmd = nil
	m.logStore.Add("meilisearch", fmt.Sprintf("Meilisearch stopped (was PID: %d)", pid))
	return nil
}

func (m *Meilisearch) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return m.Start()
}

func (m *Meilisearch) Status() services.ServiceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := services.StatusStopped
	if m.StatusText != "" {
		status = m.StatusText
	} else if m.Running {
		status = services.StatusRunning
	}

	startedAt := ""
	if !m.StartTime.IsZero() {
		startedAt = m.StartTime.Format(time.RFC3339)
	}

	return services.ServiceStatus{
		Name:      "meilisearch",
		Status:    status,
		Port:      m.ServicePort,
		Version:   m.version,
		PID:       m.ProcessPID,
		Uptime:    m.GetUptime(),
		StartedAt: startedAt,
		Error:     m.LastError,
	}
}

func (m *Meilisearch) Logs(lines int) []string {
	return m.logStore.Get("meilisearch", lines)
}

func (m *Meilisearch) Version() string { return m.version }

func (m *Meilisearch) SetVersion(version string) error {
	installPath := filepath.Join(m.paths.InstalledPath(), "meilisearch", version)
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("meilisearch: version %s not installed", version)
	}
	m.version = version
	return m.store.SaveServiceConfig("meilisearch", config.ServiceConfig{
		Name:        "meilisearch",
		Version:     version,
		InstallPath: installPath,
		Port:        m.ServicePort,
		Enabled:     true,
	})
}

func (m *Meilisearch) IsInstalled() bool {
	_, err := m.findBinary()
	return err == nil
}

// findBinary handles Meilisearch's quirky packaging: upstream releases
// the binary as a bare .exe (no zip), so the file lives at
// installed/meilisearch/<version>/meilisearch-windows-amd64.exe. We
// also accept a renamed meilisearch.exe in case the user moves it.
func (m *Meilisearch) findBinary() (string, error) {
	// Saved config wins so the user's chosen version sticks across restarts.
	cfg, err := m.store.GetServiceConfig("meilisearch")
	if err == nil && cfg.InstallPath != "" {
		for _, name := range []string{"meilisearch.exe", "meilisearch-windows-amd64.exe"} {
			bin := filepath.Join(cfg.InstallPath, name)
			if _, err := os.Stat(bin); err == nil {
				m.version = cfg.Version
				return bin, nil
			}
		}
	}

	// Fall back to scanning installed/meilisearch/<version>/.
	installedDir := filepath.Join(m.paths.InstalledPath(), "meilisearch")
	entries, err := os.ReadDir(installedDir)
	if err != nil {
		return "", fmt.Errorf("no meilisearch installation found - install it from the Packages page")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, name := range []string{"meilisearch.exe", "meilisearch-windows-amd64.exe"} {
			bin := filepath.Join(installedDir, entry.Name(), name)
			if _, err := os.Stat(bin); err == nil {
				m.version = entry.Name()
				return bin, nil
			}
		}
	}
	return "", fmt.Errorf("no meilisearch installation found - install it from the Packages page")
}

func (m *Meilisearch) waitForExit() {
	if m.cmd != nil {
		_ = m.cmd.Wait()
	}
	m.mu.Lock()
	if m.Running {
		m.Running = false
		m.StatusText = ""
		m.ProcessPID = 0
		m.logStore.Add("meilisearch", "Meilisearch process exited")
	}
	m.mu.Unlock()
}

// MasterKey exposes the dev master key so other parts of Hangar (e.g.
// a future MCP tool, or the Installed page "Open" button) can build
// authenticated requests without re-hardcoding it.
func (m *Meilisearch) MasterKey() string { return devMasterKey }
