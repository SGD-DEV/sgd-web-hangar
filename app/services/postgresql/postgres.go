package postgresql

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/logs"
	"github.com/devour-app/devour/app/services"
)

// tcpProbe returns true if a TCP connect to host:port succeeds within timeout.
// Used to verify postgres is accepting connections (more reliable than reading
// postmaster.pid, which can lie about a process that's still in WAL recovery).
func tcpProbe(host string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

type PostgreSQL struct {
	services.BaseService
	paths    config.Paths
	store    *config.Store
	logStore *logs.Store
	cmd      *exec.Cmd
	mu       sync.Mutex
	version  string
}

func New(paths config.Paths, store *config.Store, logStore *logs.Store) *PostgreSQL {
	return &PostgreSQL{
		BaseService: services.BaseService{
			ServiceName: "postgresql",
			ServicePort: 5432,
		},
		paths:    paths,
		store:    store,
		logStore: logStore,
	}
}

func (p *PostgreSQL) Name() string {
	return "postgresql"
}

func (p *PostgreSQL) Start() error {
	p.mu.Lock()

	if p.Running {
		p.mu.Unlock()
		return nil
	}

	// Set starting status immediately so the UI shows a spinner
	p.StatusText = services.StatusStarting
	p.LastError = ""

	pgCtlPath, err := p.findPgCtl()
	if err != nil {
		p.StatusText = services.StatusStopped
		p.mu.Unlock()
		return fmt.Errorf("postgresql: %w", err)
	}

	dataDir := p.getDataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		p.StatusText = services.StatusStopped
		p.mu.Unlock()
		return fmt.Errorf("postgresql: creating data dir: %w", err)
	}

	needsInit := !p.isInitialized(dataDir)

	// Release lock before potentially long-running init so Status() remains responsive
	p.mu.Unlock()

	if needsInit {
		// Surface a distinct "initializing" status so the UI shows
		// "Initializing (first run, can take 30-60s)" instead of just
		// "starting" - users were assuming Devour was hung.
		p.mu.Lock()
		p.StatusText = services.StatusInitializing
		p.mu.Unlock()
		// initDB logs "Initializing PostgreSQL data directory..." itself,
		// so don't double-log here.
		if err := p.initDB(dataDir); err != nil {
			p.mu.Lock()
			p.StatusText = services.StatusStopped
			p.LastError = err.Error()
			p.mu.Unlock()
			return fmt.Errorf("postgresql: initializing: %w", err)
		}
	}

	// Generate config if needed (no lock required, file-level operation)
	confPath := filepath.Join(dataDir, "postgresql.conf")
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		if err := p.generateConfig(confPath, dataDir); err != nil {
			p.mu.Lock()
			p.StatusText = services.StatusStopped
			p.mu.Unlock()
			return fmt.Errorf("postgresql: generating config: %w", err)
		}
	}

	logFile := filepath.Join(p.paths.LogsPath(), "postgresql.log")

	// Adopt-already-running case: if a previous Devour run started postgres
	// and exited (or this Start() is a retry after pg_ctl hung), there's
	// already a postgres.exe owning the port. Detect that, take ownership
	// of the PID, and skip the spawn entirely. Saves a "port in use" error
	// AND solves the case where pg_ctl hung in a previous attempt while
	// postgres came up fine.
	if existingPID := p.readPostmasterPID(dataDir); existingPID > 0 && tcpProbe("127.0.0.1", p.ServicePort, 500*time.Millisecond) {
		p.logStore.Add("postgresql", fmt.Sprintf("Adopted already-running PostgreSQL (PID: %d)", existingPID))
		p.mu.Lock()
		p.Running = true
		p.StatusText = services.StatusRunning
		p.ProcessPID = existingPID
		p.StartTime = time.Now()
		p.LastError = ""
		p.cmd = nil
		p.mu.Unlock()
		go p.monitorProcess(dataDir)
		return nil
	}

	// Skip pg_ctl entirely and spawn postgres.exe directly. pg_ctl on Windows
	// has a habit of hanging in its internal wait/signal-handling logic when
	// running under a restricted token, even though postgres itself is fine.
	// The user verified this: postgresql.log showed "ready to accept
	// connections" within 5s while pg_ctl's CombinedOutput never returned
	// for 20+ minutes. Direct postgres.exe avoids pg_ctl entirely - we get
	// the PID from cmd.Process.Pid, redirect stderr to the log file
	// ourselves, and probe port 5432 to confirm readiness.
	postgresExe := filepath.Join(filepath.Dir(pgCtlPath), "postgres.exe")
	if _, err := os.Stat(postgresExe); err != nil {
		p.mu.Lock()
		p.StatusText = services.StatusStopped
		p.LastError = "postgres.exe not found next to pg_ctl.exe at " + postgresExe
		p.mu.Unlock()
		return fmt.Errorf("postgresql: %s", p.LastError)
	}

	p.logStore.Add("postgresql", "Starting PostgreSQL server...")

	// postgres.exe refuses to run with an administrator token. When Hangar
	// itself is elevated, let pg_ctl start it: pg_ctl drops privileges
	// reliably, unlike a hand-built restricted token (0xc0000142). Its
	// output goes to the log file, never to a pipe - a pipe inherited by
	// the postmaster is what made pg_ctl appear to hang in the past.
	if elevated, _ := isElevated(); elevated {
		return p.startViaPgCtl(pgCtlPath, dataDir, logFile)
	}

	startCmd := exec.Command(postgresExe, "-D", dataDir)
	startCmd.Dir = filepath.Dir(postgresExe)
	services.HideWindow(startCmd)

	// Open log file for postgres' own stderr/stdout so we can show the user
	// the real startup errors when something goes wrong. O_APPEND because
	// postgres also writes its own log entries via -l-equivalent settings.
	if logFh, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		startCmd.Stdout = logFh
		startCmd.Stderr = logFh
		// NOTE: not closing logFh here - postgres holds it for its lifetime.
		// The OS reaps the file handle when postgres exits.
	}

	// Postgres refuses to run with an Administrator token. Devour usually
	// runs elevated (hosts file editing), so drop privileges before spawning.
	if err := applyRestrictedToken(startCmd); err != nil {
		p.logStore.Add("postgresql", fmt.Sprintf("Warning: could not apply restricted token (%v); postgres may refuse to start if Devour is elevated", err))
	}

	if err := startCmd.Start(); err != nil {
		p.mu.Lock()
		p.StatusText = services.StatusStopped
		p.LastError = "spawning postgres.exe: " + err.Error()
		p.mu.Unlock()
		return fmt.Errorf("postgresql: %s", p.LastError)
	}
	pid := startCmd.Process.Pid
	// Detach so postgres outlives our exec.Cmd struct. It manages its own
	// lifecycle via postmaster.pid; monitorProcess watches that file.
	go func() { _ = startCmd.Wait() }()

	// Probe port 5432 (or whatever the user configured) to confirm postgres
	// is actually accepting connections. 30s ceiling - typical cold start
	// is 1-5s, WAL recovery from a crash can take 10-20s.
	deadline := time.Now().Add(30 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		if tcpProbe("127.0.0.1", p.ServicePort, 500*time.Millisecond) {
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		// Postgres process is alive (we just forked it) but not listening.
		// Most likely a config error or an already-bound port. Kill it,
		// surface the log tail.
		_ = startCmd.Process.Kill()
		p.mu.Lock()
		p.StatusText = services.StatusStopped
		tail := readLogTail(logFile, 4096)
		if tail != "" {
			p.LastError = "PostgreSQL didn't accept connections in 30s. Last log entries:\n" + tail
		} else {
			p.LastError = "PostgreSQL didn't accept connections in 30s and no log was written"
		}
		p.mu.Unlock()
		p.logStore.Add("postgresql", p.LastError)
		return fmt.Errorf("postgresql: %s", p.LastError)
	}

	p.mu.Lock()
	p.Running = true
	p.StatusText = services.StatusRunning
	p.ProcessPID = pid
	p.StartTime = time.Now()
	p.LastError = ""
	p.cmd = nil // pg_ctl already exited; we track the server via PID
	p.mu.Unlock()

	go p.monitorProcess(dataDir)

	p.logStore.Add("postgresql", fmt.Sprintf("PostgreSQL started (PID: %d)", pid))
	return nil
}

// startViaPgCtl is the elevated-Hangar start path (see Start). pg_ctl runs
// with -W (don't wait); readiness is detected by probing the port, the PID
// comes from postmaster.pid.
func (p *PostgreSQL) startViaPgCtl(pgCtlPath, dataDir, logFile string) error {
	cmd := exec.Command(pgCtlPath, "start", "-W", "-D", dataDir, "-l", logFile)
	cmd.Dir = filepath.Dir(pgCtlPath)
	services.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		p.mu.Lock()
		p.StatusText = services.StatusStopped
		p.LastError = "pg_ctl start: " + err.Error()
		p.mu.Unlock()
		return fmt.Errorf("postgresql: %s", p.LastError)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if tcpProbe("127.0.0.1", p.ServicePort, 500*time.Millisecond) {
			pid := p.readPostmasterPID(dataDir)
			p.mu.Lock()
			p.Running = true
			p.StatusText = services.StatusRunning
			p.ProcessPID = pid
			p.StartTime = time.Now()
			p.LastError = ""
			p.cmd = nil
			p.mu.Unlock()
			go p.monitorProcess(dataDir)
			p.logStore.Add("postgresql", fmt.Sprintf("PostgreSQL started via pg_ctl (PID: %d)", pid))
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	p.mu.Lock()
	p.StatusText = services.StatusStopped
	if tail := readLogTail(logFile, 4096); tail != "" {
		p.LastError = "PostgreSQL didn't accept connections in 30s. Last log entries:\n" + tail
	} else {
		p.LastError = "PostgreSQL didn't accept connections in 30s and no log was written"
	}
	p.mu.Unlock()
	p.logStore.Add("postgresql", p.LastError)
	return fmt.Errorf("postgresql: %s", p.LastError)
}

func (p *PostgreSQL) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.Running {
		p.StatusText = ""
		return nil
	}

	p.StatusText = services.StatusStopping
	pid := p.ProcessPID

	pgCtlPath, err := p.findPgCtl()
	if err == nil {
		dataDir := p.getDataDir()
		stopCmd := exec.Command(pgCtlPath, "stop", "-w", "-D", dataDir, "-m", "fast")
		stopCmd.Dir = filepath.Dir(pgCtlPath)
		services.HideWindow(stopCmd)
		// pg_ctl drops an administrator token by itself, like on start.
		if err := stopCmd.Run(); err != nil {
			// Fallback: kill the process directly
			if pid > 0 {
				if proc, err := os.FindProcess(pid); err == nil {
					proc.Kill()
				}
			}
		}
	} else if pid > 0 {
		if proc, err := os.FindProcess(pid); err == nil {
			proc.Kill()
		}
	}

	p.Running = false
	p.StatusText = ""
	p.ProcessPID = 0
	p.StartTime = time.Time{}
	p.cmd = nil
	p.logStore.Add("postgresql", fmt.Sprintf("PostgreSQL stopped (was PID: %d)", pid))
	return nil
}

func (p *PostgreSQL) Restart() error {
	if err := p.Stop(); err != nil {
		return err
	}
	time.Sleep(1 * time.Second)
	return p.Start()
}

func (p *PostgreSQL) Status() services.ServiceStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	status := services.StatusStopped
	if p.StatusText != "" {
		status = p.StatusText
	} else if p.Running {
		status = services.StatusRunning
	}

	startedAt := ""
	if !p.StartTime.IsZero() {
		startedAt = p.StartTime.Format(time.RFC3339)
	}

	return services.ServiceStatus{
		Name:      "postgresql",
		Status:    status,
		Port:      p.ServicePort,
		Version:   p.version,
		PID:       p.ProcessPID,
		Uptime:    p.GetUptime(),
		StartedAt: startedAt,
		Error:     p.LastError,
	}
}

func (p *PostgreSQL) Logs(lines int) []string {
	return p.logStore.Get("postgresql", lines)
}

func (p *PostgreSQL) Version() string {
	return p.version
}

func (p *PostgreSQL) SetVersion(version string) error {
	installPath := p.paths.PostgreSQLPath(version)
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("postgresql: version %s not installed", version)
	}
	p.version = version
	return p.store.SaveServiceConfig("postgresql", config.ServiceConfig{
		Name:        "postgresql",
		Version:     version,
		InstallPath: installPath,
		Port:        p.ServicePort,
		DataDir:     p.getDataDir(),
		Enabled:     true,
	})
}

func (p *PostgreSQL) IsInstalled() bool {
	_, err := p.findPgCtl()
	return err == nil
}

func (p *PostgreSQL) GetConnectionString() string {
	return fmt.Sprintf("postgresql://postgres@127.0.0.1:%d/postgres", p.ServicePort)
}

func (p *PostgreSQL) findPgCtl() (string, error) {
	cfg, err := p.store.GetServiceConfig("postgresql")
	if err == nil && cfg.InstallPath != "" {
		if found := p.findPgCtlIn(cfg.InstallPath); found != "" {
			p.version = cfg.Version
			p.ServicePort = cfg.Port
			return found, nil
		}
	}

	installedDir := filepath.Join(p.paths.InstalledPath(), "postgresql")
	entries, err := os.ReadDir(installedDir)
	if err != nil {
		return "", fmt.Errorf("no postgresql installation found")
	}

	for _, entry := range entries {
		if entry.IsDir() {
			versionDir := filepath.Join(installedDir, entry.Name())
			if found := p.findPgCtlIn(versionDir); found != "" {
				p.version = entry.Name()
				return found, nil
			}
		}
	}

	return "", fmt.Errorf("no postgresql installation found")
}

func (p *PostgreSQL) findPgCtlIn(dir string) string {
	// Check common layouts: dir/pgsql/bin/, dir/bin/, dir/<subdir>/pgsql/bin/, dir/<subdir>/bin/
	candidates := []string{
		filepath.Join(dir, "pgsql", "bin", "pg_ctl.exe"),
		filepath.Join(dir, "bin", "pg_ctl.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	// Check one-level nested subdirectories (ZIP may extract to a subdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			nested := []string{
				filepath.Join(dir, e.Name(), "pgsql", "bin", "pg_ctl.exe"),
				filepath.Join(dir, e.Name(), "bin", "pg_ctl.exe"),
			}
			for _, c := range nested {
				if _, err := os.Stat(c); err == nil {
					return c
				}
			}
		}
	}
	return ""
}

func (p *PostgreSQL) getDataDir() string {
	if p.version != "" {
		return filepath.Join(p.paths.DataPath(), "pg-data", p.version)
	}
	return filepath.Join(p.paths.DataPath(), "pg-data", "default")
}

func (p *PostgreSQL) isInitialized(dataDir string) bool {
	pgVersionFile := filepath.Join(dataDir, "PG_VERSION")
	_, err := os.Stat(pgVersionFile)
	return err == nil
}

func (p *PostgreSQL) initDB(dataDir string) error {
	// initdb refuses to run against a non-empty data dir. PG_VERSION is written
	// last, so a partial/failed prior init leaves the dir non-empty AND
	// uninitialized — every retry would then fail. Wipe contents first.
	if isDirNonEmpty(dataDir) && !p.isInitialized(dataDir) {
		p.logStore.Add("postgresql", "Cleaning stale partial-init data directory before re-initializing")
		if err := cleanDirContents(dataDir); err != nil {
			return fmt.Errorf("cleaning partial-init data dir: %w", err)
		}
	}

	p.logStore.Add("postgresql", "Initializing PostgreSQL data directory...")

	// Find initdb.exe — it lives in the same bin/ directory as pg_ctl.exe
	pgCtlPath, err := p.findPgCtl()
	if err != nil {
		return fmt.Errorf("finding pg_ctl for initdb: %w", err)
	}
	initdbPath := filepath.Join(filepath.Dir(pgCtlPath), "initdb.exe")
	if _, err := os.Stat(initdbPath); os.IsNotExist(err) {
		return fmt.Errorf("initdb.exe not found at %s", initdbPath)
	}

	// Determine the share directory for -L flag (contains postgres.bki)
	binDir := filepath.Dir(initdbPath)
	baseDir := filepath.Dir(binDir) // parent of bin/
	shareDir := filepath.Join(baseDir, "share")

	args := []string{"-D", dataDir, "-U", "postgres", "--encoding=UTF8"}
	if _, err := os.Stat(filepath.Join(shareDir, "postgres.bki")); err == nil {
		args = append(args, "-L", shareDir)
	}

	cmd := exec.Command(initdbPath, args...)
	cmd.Dir = filepath.Dir(initdbPath)
	services.HideWindow(cmd)
	// No custom token here: when started from an elevated process initdb
	// re-executes itself under a restricted token on its own (the same
	// mechanism pg_ctl uses). Launching it with Hangar's hand-built token
	// failed with STATUS_DLL_INIT_FAILED (0xc0000142).
	// Log what we're actually running - if init fails silently this is the
	// only way to debug it from the UI.
	p.logStore.Add("postgresql", fmt.Sprintf("Running: %s %v (cwd=%s)", initdbPath, args, cmd.Dir))

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Always include err.Error() so the user sees something even when
		// initdb produced no stdout/stderr (e.g. exit status 1 with no msg,
		// or token-related launch failure where the child never ran).
		msg := "PostgreSQL initialization failed: " + err.Error()
		if len(output) > 0 {
			snippet := string(output)
			if len(snippet) > 2048 {
				snippet = snippet[:2048] + "...(truncated)"
			}
			msg += "\n" + snippet
		} else {
			msg += " (no stdout/stderr - child may have failed to launch)"
		}
		p.logStore.Add("postgresql", msg)
		return fmt.Errorf("initdb: %s: %w", string(output), err)
	}
	p.logStore.Add("postgresql", "PostgreSQL data directory initialized")
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

// readPostmasterPID reads the PostgreSQL server PID from the postmaster.pid file.
// The first line of this file contains the PID.
func (p *PostgreSQL) readPostmasterPID(dataDir string) int {
	pidFile := filepath.Join(dataDir, "postmaster.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0
	}
	lines := strings.SplitN(string(data), "\n", 2)
	if len(lines) == 0 {
		return 0
	}
	pid := 0
	fmt.Sscanf(strings.TrimSpace(lines[0]), "%d", &pid)
	return pid
}

// monitorProcess periodically checks if the PostgreSQL server is still running
// by checking for the postmaster.pid file. When it disappears, the server has stopped.
func (p *PostgreSQL) monitorProcess(dataDir string) {
	pidFile := filepath.Join(dataDir, "postmaster.pid")
	for {
		time.Sleep(2 * time.Second)
		if _, err := os.Stat(pidFile); os.IsNotExist(err) {
			p.mu.Lock()
			if p.Running {
				p.Running = false
				p.StatusText = ""
				p.ProcessPID = 0
				p.logStore.Add("postgresql", "PostgreSQL process exited")
			}
			p.mu.Unlock()
			return
		}
	}
}

func (p *PostgreSQL) generateConfig(confPath, dataDir string) error {
	data := map[string]interface{}{
		"Port":    p.ServicePort,
		"DataDir": filepath.ToSlash(dataDir),
		"LogDir":  filepath.ToSlash(p.paths.LogsPath()),
	}

	return renderTemplateStr(postgresqlConfTmpl, confPath, data)
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

// readLogTail returns up to maxBytes from the end of path. Used to surface
// the actual startup error from postgresql.log when our wait-for-ready
// poll times out.
func readLogTail(path string, maxBytes int) string {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	offset := int64(0)
	readSize := info.Size()
	if readSize > int64(maxBytes) {
		offset = readSize - int64(maxBytes)
		readSize = int64(maxBytes)
	}
	buf := make([]byte, readSize)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return ""
	}
	return strings.TrimSpace(string(buf))
}
