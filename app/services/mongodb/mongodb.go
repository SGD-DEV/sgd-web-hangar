// Package mongodb wraps MongoDB Community Server as a Hangar service.
// The MongoDB Windows zip ships bin/mongod.exe plus a long tail of
// supporting binaries (mongo, mongosh, mongoexport, ...). We launch
// mongod with --dbpath and --port and let those other tools live on
// PATH for users who want them.
//
// First Start() bootstraps the data dir under
// %LOCALAPPDATA%\Hangar\data\mongodb. mongod is happy to initialise
// itself on an empty dir; we just need to create the dir and let it
// do its thing. Default port 27017.
package mongodb

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

type MongoDB struct {
	services.BaseService
	paths    config.Paths
	store    *config.Store
	logStore *logs.Store
	cmd      *exec.Cmd
	mu       sync.Mutex
	version  string
}

func New(paths config.Paths, store *config.Store, logStore *logs.Store) *MongoDB {
	return &MongoDB{
		BaseService: services.BaseService{
			ServiceName: "mongodb",
			ServicePort: 27017,
		},
		paths:    paths,
		store:    store,
		logStore: logStore,
	}
}

func (m *MongoDB) Name() string { return "mongodb" }

func (m *MongoDB) Start() error {
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
		return fmt.Errorf("mongodb: %w", err)
	}

	dataDir := filepath.Join(m.paths.DataPath(), "mongodb")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		m.StatusText = services.StatusStopped
		return fmt.Errorf("mongodb: creating data dir: %w", err)
	}
	logDir := filepath.Join(m.paths.LogsPath())
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		m.StatusText = services.StatusStopped
		return fmt.Errorf("mongodb: creating log dir: %w", err)
	}

	m.cmd = exec.Command(binPath,
		"--dbpath", dataDir,
		"--port", fmt.Sprintf("%d", m.ServicePort),
		"--bind_ip", "127.0.0.1",
		"--logpath", filepath.Join(logDir, "mongodb.log"),
		"--logappend",
		// First-run is slow on Windows because mongod allocates the
		// WiredTiger cache; emit an initializing status so the UI
		// doesn't flash "starting" -> "error" if we poke status too
		// early.
	)
	m.cmd.Dir = filepath.Dir(binPath)
	services.HideWindow(m.cmd)

	if err := m.cmd.Start(); err != nil {
		m.StatusText = services.StatusStopped
		m.LastError = err.Error()
		return fmt.Errorf("mongodb: starting: %w", err)
	}

	m.Running = true
	m.StatusText = services.StatusRunning
	m.ProcessPID = m.cmd.Process.Pid
	m.StartTime = time.Now()
	go m.waitForExit()

	m.logStore.Add("mongodb", fmt.Sprintf("MongoDB started (PID: %d) — :%d dbpath=%s", m.ProcessPID, m.ServicePort, dataDir))
	return nil
}

func (m *MongoDB) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.Running || m.cmd == nil || m.cmd.Process == nil {
		m.Running = false
		m.StatusText = ""
		return nil
	}
	m.StatusText = services.StatusStopping
	pid := m.cmd.Process.Pid

	// MongoDB responds to SIGINT (Ctrl-C) with a clean shutdown that
	// flushes the WiredTiger journal. On Windows os.Process.Kill is
	// the only universal signal available, but for mongod that
	// translates to a hard kill - it will replay the journal on next
	// start. Acceptable for a local-dev service.
	_ = m.cmd.Process.Kill()

	done := make(chan struct{})
	go func() { _ = m.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		m.logStore.Add("mongodb", "MongoDB did not stop in time, force killed (journal replay on next start)")
	}

	m.Running = false
	m.StatusText = ""
	m.ProcessPID = 0
	m.StartTime = time.Time{}
	m.cmd = nil
	m.logStore.Add("mongodb", fmt.Sprintf("MongoDB stopped (was PID: %d)", pid))
	return nil
}

func (m *MongoDB) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return m.Start()
}

func (m *MongoDB) Status() services.ServiceStatus {
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
		Name:      "mongodb",
		Status:    status,
		Port:      m.ServicePort,
		Version:   m.version,
		PID:       m.ProcessPID,
		Uptime:    m.GetUptime(),
		StartedAt: startedAt,
		Error:     m.LastError,
	}
}

func (m *MongoDB) Logs(lines int) []string {
	return m.logStore.Get("mongodb", lines)
}

func (m *MongoDB) Version() string { return m.version }

func (m *MongoDB) SetVersion(version string) error {
	installPath := filepath.Join(m.paths.InstalledPath(), "mongodb", version)
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("mongodb: version %s not installed", version)
	}
	m.version = version
	return m.store.SaveServiceConfig("mongodb", config.ServiceConfig{
		Name:        "mongodb",
		Version:     version,
		InstallPath: installPath,
		Port:        m.ServicePort,
		Enabled:     true,
	})
}

func (m *MongoDB) IsInstalled() bool {
	_, err := m.findBinary()
	return err == nil
}

// findBinary walks the MongoDB zip layout: the archive extracts as
// mongodb-windows-x86_64-X.Y.Z/bin/mongod.exe, so after our extractor
// runs the binary sits at
// installed/mongodb/<version>/mongodb-windows-x86_64-<version>/bin/mongod.exe.
// We also accept a flat installed/mongodb/<version>/bin/mongod.exe
// path in case the user re-arranged.
func (m *MongoDB) findBinary() (string, error) {
	candidates := []string{}

	if cfg, err := m.store.GetServiceConfig("mongodb"); err == nil && cfg.InstallPath != "" {
		candidates = append(candidates,
			filepath.Join(cfg.InstallPath, "bin", "mongod.exe"),
		)
		// Nested layout from the official zip.
		entries, _ := os.ReadDir(cfg.InstallPath)
		for _, e := range entries {
			if e.IsDir() {
				candidates = append(candidates, filepath.Join(cfg.InstallPath, e.Name(), "bin", "mongod.exe"))
			}
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				m.version = cfg.Version
				return p, nil
			}
		}
	}

	// Scan installed/mongodb/* for either flat or nested layout.
	installedDir := filepath.Join(m.paths.InstalledPath(), "mongodb")
	versionEntries, err := os.ReadDir(installedDir)
	if err != nil {
		return "", fmt.Errorf("no mongodb installation found - install it from the Packages page")
	}
	for _, ve := range versionEntries {
		if !ve.IsDir() {
			continue
		}
		versionDir := filepath.Join(installedDir, ve.Name())

		// Flat: installed/mongodb/8.0.23/bin/mongod.exe
		if bin := filepath.Join(versionDir, "bin", "mongod.exe"); fileExists(bin) {
			m.version = ve.Name()
			return bin, nil
		}
		// Nested: installed/mongodb/8.0.23/mongodb-windows-x86_64-8.0.23/bin/mongod.exe
		inner, _ := os.ReadDir(versionDir)
		for _, i := range inner {
			if !i.IsDir() {
				continue
			}
			if bin := filepath.Join(versionDir, i.Name(), "bin", "mongod.exe"); fileExists(bin) {
				m.version = ve.Name()
				return bin, nil
			}
		}
	}
	return "", fmt.Errorf("no mongodb installation found - install it from the Packages page")
}

func (m *MongoDB) waitForExit() {
	if m.cmd != nil {
		_ = m.cmd.Wait()
	}
	m.mu.Lock()
	if m.Running {
		m.Running = false
		m.StatusText = ""
		m.ProcessPID = 0
		m.logStore.Add("mongodb", "MongoDB process exited")
	}
	m.mu.Unlock()
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
