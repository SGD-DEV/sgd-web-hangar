package mysql

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"text/template"
	"time"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/logs"
	"github.com/devour-app/devour/app/services"
)

type MySQL struct {
	services.BaseService
	paths    config.Paths
	store    *config.Store
	logStore *logs.Store
	cmd      *exec.Cmd
	mu       sync.Mutex
	version  string
}

func New(paths config.Paths, store *config.Store, logStore *logs.Store) *MySQL {
	return &MySQL{
		BaseService: services.BaseService{
			ServiceName: "mysql",
			ServicePort: 3306,
		},
		paths:    paths,
		store:    store,
		logStore: logStore,
	}
}

func (m *MySQL) Name() string {
	return "mysql"
}

func (m *MySQL) Start() error {
	m.mu.Lock()

	if m.Running {
		m.mu.Unlock()
		return nil
	}

	// Set starting status immediately so the UI shows a spinner
	m.StatusText = services.StatusStarting
	m.LastError = ""

	mysqldPath, err := m.findMysqld()
	if err != nil {
		m.StatusText = services.StatusStopped
		m.mu.Unlock()
		return fmt.Errorf("mysql: %w", err)
	}

	dataDir := m.getDataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		m.StatusText = services.StatusStopped
		m.mu.Unlock()
		return fmt.Errorf("mysql: creating data dir: %w", err)
	}

	// Kill any stale mysqld processes that might be locking our data directory
	m.killStaleMysqld()

	iniPath := filepath.Join(m.paths.ConfPath("mysql"), "my.ini")
	// Always regenerate config to ensure paths are correct
	if err := m.generateConfig(iniPath, dataDir); err != nil {
		m.StatusText = services.StatusStopped
		m.mu.Unlock()
		return fmt.Errorf("mysql: generating config: %w", err)
	}

	needsInit := !m.isInitialized(dataDir)

	// Release lock before potentially long-running init so Status() remains responsive
	m.mu.Unlock()

	if needsInit {
		// Promote status from "starting" to "initializing" so the UI shows
		// the slow first-run data-dir bootstrap distinctly.
		m.mu.Lock()
		m.StatusText = services.StatusInitializing
		m.mu.Unlock()
		m.logStore.Add("mysql", "Initializing MySQL data directory (this may take 1-2 minutes)...")
		if err := m.initializeDataDir(mysqldPath, iniPath, dataDir); err != nil {
			m.mu.Lock()
			m.StatusText = services.StatusStopped
			m.LastError = err.Error()
			m.mu.Unlock()
			return fmt.Errorf("mysql: initializing data dir: %w", err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Write init SQL that grants root access via TCP (idempotent).
	// --init-file runs every startup before accepting connections — no race condition.
	initSQL := m.writeInitSQL()

	args := []string{fmt.Sprintf("--defaults-file=%s", iniPath)}
	if initSQL != "" {
		args = append(args, fmt.Sprintf("--init-file=%s", initSQL))
	}

	m.cmd = exec.Command(mysqldPath, args...)
	m.cmd.Dir = filepath.Dir(mysqldPath)
	services.HideWindow(m.cmd)

	if err := m.cmd.Start(); err != nil {
		m.StatusText = services.StatusStopped
		m.LastError = err.Error()
		return fmt.Errorf("mysql: starting: %w", err)
	}

	m.Running = true
	m.StatusText = services.StatusRunning
	m.ProcessPID = m.cmd.Process.Pid
	m.StartTime = time.Now()
	m.LastError = ""

	go m.waitForExit()

	m.logStore.Add("mysql", fmt.Sprintf("MySQL started (PID: %d)", m.ProcessPID))
	return nil
}

func (m *MySQL) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.Running || m.cmd == nil || m.cmd.Process == nil {
		m.Running = false
		m.StatusText = ""
		return nil
	}

	m.StatusText = services.StatusStopping

	pid := m.cmd.Process.Pid

	// Try graceful shutdown via mysqladmin over TCP
	mysqladminPath := filepath.Join(filepath.Dir(m.cmd.Path), "mysqladmin.exe")
	if _, err := os.Stat(mysqladminPath); err == nil {
		stopCmd := exec.Command(mysqladminPath,
			"-u", "root",
			fmt.Sprintf("--host=127.0.0.1"),
			fmt.Sprintf("--port=%d", m.ServicePort),
			"shutdown")
		services.HideWindow(stopCmd)
		_ = stopCmd.Run()
	}

	// Wait up to 10 seconds for process to exit
	done := make(chan struct{})
	go func() {
		if m.cmd != nil && m.cmd.Process != nil {
			m.cmd.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
		// Clean exit
	case <-time.After(10 * time.Second):
		// Force kill
		m.logStore.Add("mysql", "MySQL did not stop gracefully, force killing...")
		if m.cmd != nil && m.cmd.Process != nil {
			m.cmd.Process.Kill()
		}
		<-done
	}

	m.Running = false
	m.StatusText = ""
	m.ProcessPID = 0
	m.StartTime = time.Time{}
	m.cmd = nil
	m.logStore.Add("mysql", fmt.Sprintf("MySQL stopped (was PID: %d)", pid))
	return nil
}

func (m *MySQL) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	time.Sleep(1 * time.Second)
	return m.Start()
}

func (m *MySQL) Status() services.ServiceStatus {
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
		Name:      "mysql",
		Status:    status,
		Port:      m.ServicePort,
		Version:   m.version,
		PID:       m.ProcessPID,
		Uptime:    m.GetUptime(),
		StartedAt: startedAt,
		Error:     m.LastError,
	}
}

func (m *MySQL) Logs(lines int) []string {
	return m.logStore.Get("mysql", lines)
}

func (m *MySQL) Version() string {
	return m.version
}

func (m *MySQL) SetVersion(version string) error {
	installPath := m.paths.MySQLPath(version)
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("mysql: version %s not installed", version)
	}
	m.version = version
	return m.store.SaveServiceConfig("mysql", config.ServiceConfig{
		Name:        "mysql",
		Version:     version,
		InstallPath: installPath,
		Port:        m.ServicePort,
		DataDir:     m.getDataDir(),
		Enabled:     true,
	})
}

func (m *MySQL) IsInstalled() bool {
	_, err := m.findMysqld()
	return err == nil
}

func (m *MySQL) GetConnectionString() string {
	return fmt.Sprintf("mysql://root@127.0.0.1:%d", m.ServicePort)
}

// EnsureConfig generates my.ini if it doesn't exist yet (called on app startup)
func (m *MySQL) EnsureConfig() error {
	iniPath := filepath.Join(m.paths.ConfPath("mysql"), "my.ini")
	if _, err := os.Stat(iniPath); err == nil {
		return nil // already exists
	}

	if _, err := m.findMysqld(); err != nil {
		return err
	}
	dataDir := m.getDataDir()
	os.MkdirAll(dataDir, 0755)
	return m.generateConfig(iniPath, dataDir)
}

func (m *MySQL) findMysqld() (string, error) {
	cfg, err := m.store.GetServiceConfig("mysql")
	if err == nil && cfg.InstallPath != "" {
		if found := m.findMysqldIn(cfg.InstallPath); found != "" {
			m.version = cfg.Version
			m.ServicePort = cfg.Port
			return found, nil
		}
	}

	installedDir := filepath.Join(m.paths.InstalledPath(), "mysql")
	entries, err := os.ReadDir(installedDir)
	if err != nil {
		return "", fmt.Errorf("no mysql installation found")
	}

	for _, entry := range entries {
		if entry.IsDir() {
			versionDir := filepath.Join(installedDir, entry.Name())
			if found := m.findMysqldIn(versionDir); found != "" {
				m.version = entry.Name()
				return found, nil
			}
		}
	}

	return "", fmt.Errorf("no mysql installation found")
}

func (m *MySQL) findMysqldIn(dir string) string {
	// Direct: dir/bin/mysqld.exe
	candidate := filepath.Join(dir, "bin", "mysqld.exe")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Nested one level (ZIP extracts to subdir like mysql-9.4.0-winx64/)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			candidate = filepath.Join(dir, e.Name(), "bin", "mysqld.exe")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

func (m *MySQL) getDataDir() string {
	if m.version != "" {
		return filepath.Join(m.paths.DataPath(), "mysql-data", m.version)
	}
	return filepath.Join(m.paths.DataPath(), "mysql-data", "default")
}

func (m *MySQL) isInitialized(dataDir string) bool {
	// Check for the mysql system database directory — presence means init succeeded
	mysqlSysDir := filepath.Join(dataDir, "mysql")
	if info, err := os.Stat(mysqlSysDir); err == nil && info.IsDir() {
		// Also verify it has actual content (not just empty dir)
		entries, err := os.ReadDir(mysqlSysDir)
		if err == nil && len(entries) > 5 {
			return true
		}
	}
	return false
}

func (m *MySQL) initializeDataDir(mysqldPath, iniPath, dataDir string) error {
	// mysqld --initialize-insecure refuses to run against a non-empty datadir.
	// A previous failed init (bad my.ini option, antivirus, disk hiccup) leaves
	// ibdata1, redo logs, certs, etc. in the dir, which would brick every retry.
	// If the dir has content but isn't a valid initialized install, wipe contents.
	if isDirNonEmpty(dataDir) && !m.isInitialized(dataDir) {
		m.logStore.Add("mysql", "Cleaning stale partial-init data directory before re-initializing")
		if err := cleanDirContents(dataDir); err != nil {
			return fmt.Errorf("cleaning partial-init data dir: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, mysqldPath, fmt.Sprintf("--defaults-file=%s", iniPath), "--initialize-insecure")
	cmd.Dir = filepath.Dir(mysqldPath)
	services.HideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		m.logStore.Add("mysql", "ERROR: MySQL initialization timed out after 5 minutes")
		return fmt.Errorf("initialization timed out after 5 minutes — check if mysqld.exe is valid")
	}
	if err != nil {
		snippet := string(output)
		if len(snippet) > 2048 {
			snippet = snippet[:2048] + "...(truncated)"
		}
		m.logStore.Add("mysql", fmt.Sprintf("MySQL initialization failed: %s", snippet))
		return fmt.Errorf("initializing: %s: %w", string(output), err)
	}
	m.logStore.Add("mysql", "MySQL data directory initialized successfully")
	return nil
}

// isDirNonEmpty returns true if dir exists and contains at least one entry.
func isDirNonEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// cleanDirContents removes every child of dir but keeps dir itself, so any
// open handle/watcher on the directory remains valid (matters on Windows).
func cleanDirContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// killStaleMysqld finds and kills any leftover mysqld.exe processes that might
// be holding locks on our data directory from a previous unclean shutdown.
func (m *MySQL) killStaleMysqld() {
	// Use taskkill to find mysqld processes. On Windows, multiple mysqld
	// instances can coexist only if they use different ports/data dirs.
	// We kill ALL mysqld.exe processes since Devour is the sole MySQL manager.
	cmd := exec.Command("taskkill", "/F", "/IM", "mysqld.exe")
	services.HideWindow(cmd)
	cmd.Run() // ignore errors - no process found is fine

	// Give OS time to release file locks
	time.Sleep(1 * time.Second)
}

// writeInitSQL writes an idempotent SQL file that creates root TCP users.
// MySQL --init-file runs this on every startup, before accepting client connections.
func (m *MySQL) writeInitSQL() string {
	sqlContent := `-- Devour: grant root access via TCP (idempotent)
CREATE USER IF NOT EXISTS 'root'@'127.0.0.1' IDENTIFIED BY '';
CREATE USER IF NOT EXISTS 'root'@'%' IDENTIFIED BY '';
GRANT ALL PRIVILEGES ON *.* TO 'root'@'127.0.0.1' WITH GRANT OPTION;
GRANT ALL PRIVILEGES ON *.* TO 'root'@'%' WITH GRANT OPTION;
FLUSH PRIVILEGES;
`
	initPath := filepath.Join(m.paths.ConfPath("mysql"), "init-grants.sql")
	if err := os.MkdirAll(filepath.Dir(initPath), 0755); err != nil {
		return ""
	}
	if err := os.WriteFile(initPath, []byte(sqlContent), 0644); err != nil {
		return ""
	}
	return initPath
}

func (m *MySQL) waitForExit() {
	if m.cmd != nil {
		m.cmd.Wait()
	}
	m.mu.Lock()
	m.Running = false
	m.ProcessPID = 0
	m.mu.Unlock()
	m.logStore.Add("mysql", "MySQL process exited")
}

func (m *MySQL) generateConfig(iniPath, dataDir string) error {
	data := map[string]interface{}{
		"Port":    m.ServicePort,
		"DataDir": filepath.ToSlash(dataDir),
		"LogDir":  filepath.ToSlash(m.paths.LogsPath()),
	}

	return renderTemplateStr(myIniTmpl, iniPath, data)
}

func renderTemplateStr(tmplContent, outPath string, data interface{}) error {
	tmpl, err := template.New("tmpl").Parse(tmplContent)
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}

	outDir := filepath.Dir(outPath)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output file %s: %w", outPath, err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	return nil
}
