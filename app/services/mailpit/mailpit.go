package mailpit

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

type Mailpit struct {
	services.BaseService
	paths    config.Paths
	store    *config.Store
	logStore *logs.Store
	cmd      *exec.Cmd
	mu       sync.Mutex
	version  string
}

func New(paths config.Paths, store *config.Store, logStore *logs.Store) *Mailpit {
	return &Mailpit{
		BaseService: services.BaseService{
			ServiceName: "mailpit",
			ServicePort: 8025,
		},
		paths:    paths,
		store:    store,
		logStore: logStore,
	}
}

func (m *Mailpit) Name() string {
	return "mailpit"
}

func (m *Mailpit) Start() error {
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
		return fmt.Errorf("mailpit: %w", err)
	}

	// Mailpit args: SMTP on 1025, HTTP UI on 8025. Loopback only: neither
	// port has a password, so anyone on the LAN could read the caught mails.
	m.cmd = exec.Command(binPath,
		"--smtp", "127.0.0.1:1025",
		"--listen", "127.0.0.1:8025",
	)
	m.cmd.Dir = filepath.Dir(binPath)
	services.HideWindow(m.cmd)

	if err := m.cmd.Start(); err != nil {
		m.StatusText = services.StatusStopped
		m.LastError = err.Error()
		return fmt.Errorf("mailpit: starting: %w", err)
	}

	m.Running = true
	m.StatusText = services.StatusRunning
	m.ProcessPID = m.cmd.Process.Pid
	m.StartTime = time.Now()
	m.LastError = ""

	go m.waitForExit()

	m.logStore.Add("mailpit", fmt.Sprintf("Mailpit started (PID: %d) — SMTP :1025, Web UI :8025", m.ProcessPID))
	return nil
}

func (m *Mailpit) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.Running || m.cmd == nil || m.cmd.Process == nil {
		m.Running = false
		m.StatusText = ""
		return nil
	}

	m.StatusText = services.StatusStopping
	pid := m.cmd.Process.Pid

	// Kill the process (Mailpit doesn't have a graceful shutdown command)
	if m.cmd.Process != nil {
		m.cmd.Process.Kill()
	}

	// Wait for exit
	done := make(chan struct{})
	go func() {
		if m.cmd != nil {
			m.cmd.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		m.logStore.Add("mailpit", "Mailpit did not stop in time, force killed")
	}

	m.Running = false
	m.StatusText = ""
	m.ProcessPID = 0
	m.StartTime = time.Time{}
	m.cmd = nil
	m.logStore.Add("mailpit", fmt.Sprintf("Mailpit stopped (was PID: %d)", pid))
	return nil
}

func (m *Mailpit) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return m.Start()
}

func (m *Mailpit) Status() services.ServiceStatus {
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
		Name:      "mailpit",
		Status:    status,
		Port:      m.ServicePort,
		Version:   m.version,
		PID:       m.ProcessPID,
		Uptime:    m.GetUptime(),
		StartedAt: startedAt,
		Error:     m.LastError,
	}
}

func (m *Mailpit) Logs(lines int) []string {
	return m.logStore.Get("mailpit", lines)
}

func (m *Mailpit) Version() string {
	return m.version
}

func (m *Mailpit) SetVersion(version string) error {
	installPath := filepath.Join(m.paths.InstalledPath(), "mailpit", version)
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("mailpit: version %s not installed", version)
	}
	m.version = version
	return m.store.SaveServiceConfig("mailpit", config.ServiceConfig{
		Name:        "mailpit",
		Version:     version,
		InstallPath: installPath,
		Port:        m.ServicePort,
		Enabled:     true,
	})
}

func (m *Mailpit) IsInstalled() bool {
	_, err := m.findBinary()
	return err == nil
}

func (m *Mailpit) GetSMTPPort() int {
	return 1025
}

func (m *Mailpit) GetWebUIPort() int {
	return 8025
}

func (m *Mailpit) findBinary() (string, error) {
	// Check saved config first
	cfg, err := m.store.GetServiceConfig("mailpit")
	if err == nil && cfg.InstallPath != "" {
		bin := filepath.Join(cfg.InstallPath, "mailpit.exe")
		if _, err := os.Stat(bin); err == nil {
			m.version = cfg.Version
			return bin, nil
		}
	}

	// Scan installed directory
	installedDir := filepath.Join(m.paths.InstalledPath(), "mailpit")
	entries, err := os.ReadDir(installedDir)
	if err != nil {
		return "", fmt.Errorf("no mailpit installation found")
	}

	for _, entry := range entries {
		if entry.IsDir() {
			bin := filepath.Join(installedDir, entry.Name(), "mailpit.exe")
			if _, err := os.Stat(bin); err == nil {
				m.version = entry.Name()
				return bin, nil
			}
		}
	}

	return "", fmt.Errorf("no mailpit installation found")
}

func (m *Mailpit) waitForExit() {
	if m.cmd != nil {
		m.cmd.Wait()
	}
	m.mu.Lock()
	if m.Running {
		m.Running = false
		m.StatusText = ""
		m.ProcessPID = 0
		m.logStore.Add("mailpit", "Mailpit process exited")
	}
	m.mu.Unlock()
}
