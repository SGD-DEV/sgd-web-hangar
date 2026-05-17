package apache

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/logs"
	"github.com/devour-app/devour/app/portinfo"
	"github.com/devour-app/devour/app/services"
)

// parseApacheBindFailedPort extracts the port from an Apache config-test error.
// Apache reports bind failures as e.g.
//
//	(OS 10048)Only one usage of each socket address ...
//	make_sock: could not bind to address [::]:80
//
// We look for the make_sock line. Returns 0,false if the error wasn't a bind.
func parseApacheBindFailedPort(errMsg string) (int, bool) {
	const marker = "could not bind to address "
	i := strings.Index(errMsg, marker)
	if i < 0 {
		return 0, false
	}
	tail := strings.TrimSpace(errMsg[i+len(marker):])
	// tail starts with "0.0.0.0:80" or "[::]:443" then maybe more text.
	endIdx := strings.IndexAny(tail, " \r\n")
	addr := tail
	if endIdx > 0 {
		addr = tail[:endIdx]
	}
	colonIdx := strings.LastIndex(addr, ":")
	if colonIdx < 0 || colonIdx == len(addr)-1 {
		return 0, false
	}
	portStr := addr[colonIdx+1:]
	var port int
	for _, c := range portStr {
		if c < '0' || c > '9' {
			return 0, false
		}
		port = port*10 + int(c-'0')
	}
	if port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

type Apache struct {
	services.BaseService
	paths    config.Paths
	store    *config.Store
	logStore *logs.Store
	cmd      *exec.Cmd
	mu       sync.Mutex
	version  string
}

func New(paths config.Paths, store *config.Store, logStore *logs.Store) *Apache {
	return &Apache{
		BaseService: services.BaseService{
			ServiceName: "apache",
			ServicePort: 80,
		},
		paths:    paths,
		store:    store,
		logStore: logStore,
	}
}

func (a *Apache) Name() string {
	return "apache"
}

func (a *Apache) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.Running {
		return nil
	}

	httpdPath, err := a.findHttpd()
	if err != nil {
		return fmt.Errorf("apache: %w", err)
	}

	// ServerRoot is the Apache install dir containing bin/, modules/, conf/
	// httpdPath is typically .../Apache24/bin/httpd.exe → go up two levels
	serverRoot := filepath.Dir(filepath.Dir(httpdPath))

	// Read port from config
	if cfg, err := a.store.GetAppConfig(); err == nil {
		if cfg.ApachePort > 0 {
			a.ServicePort = cfg.ApachePort
		}
	}

	// Ensure the default welcome page exists in data/www/
	if err := services.EnsureWelcomePage(a.paths.WwwPath(), a.paths.ProjectsPath()); err != nil {
		a.logStore.Add("apache", fmt.Sprintf("Warning: could not write welcome page: %v", err))
	}

	confPath := filepath.Join(a.paths.ConfPath("apache"), "httpd.conf")
	// Always regenerate config to ensure ServerRoot and paths are correct
	os.MkdirAll(filepath.Join(a.paths.ConfPath("apache"), "vhosts"), 0755)
	if err := a.generateConfig(confPath, serverRoot); err != nil {
		return fmt.Errorf("apache: generating config: %w", err)
	}

	// Pre-flight: `httpd -t` validates the config and returns clear errors
	// for bind() conflicts, syntax errors, missing modules, etc. Without this
	// httpd will spawn, fail silently, and we'd see only "Apache process
	// exited" in the log.
	testCmd := exec.Command(httpdPath, "-f", confPath, "-t")
	testCmd.Dir = filepath.Dir(httpdPath)
	services.HideWindow(testCmd)
	if testOutput, testErr := testCmd.CombinedOutput(); testErr != nil {
		errMsg := strings.TrimSpace(string(testOutput))
		// Surface port owner so the frontend can render a kill button. Apache
		// reports bind errors as "(OS 10048)Only one usage..." plus a
		// preceding "make_sock: could not bind to address [::]:80" line.
		if port, ok := parseApacheBindFailedPort(errMsg); ok {
			if owner, _ := portinfo.OwnerOfPort(port); owner != nil {
				errMsg += fmt.Sprintf(" [port=%d pid=%d name=%s]", owner.Port, owner.PID, owner.Name)
			}
		}
		a.LastError = errMsg
		a.logStore.Add("apache", "config check failed: "+errMsg)
		return fmt.Errorf("apache: %s", errMsg)
	}

	a.cmd = exec.Command(httpdPath, "-f", confPath)
	a.cmd.Dir = filepath.Dir(httpdPath)
	services.HideWindow(a.cmd)

	if err := a.cmd.Start(); err != nil {
		a.LastError = err.Error()
		return fmt.Errorf("apache: starting: %w", err)
	}

	a.Running = true
	a.ProcessPID = a.cmd.Process.Pid
	a.StartTime = time.Now()
	a.LastError = ""

	go a.waitForExit()

	a.logStore.Add("apache", fmt.Sprintf("Apache started (PID: %d)", a.ProcessPID))
	return nil
}

func (a *Apache) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.Running || a.cmd == nil || a.cmd.Process == nil {
		a.Running = false
		return nil
	}

	if err := a.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("apache: stopping: %w", err)
	}

	a.Running = false
	a.ProcessPID = 0
	a.StartTime = time.Time{}
	a.logStore.Add("apache", "Apache stopped")
	return nil
}

func (a *Apache) Restart() error {
	if err := a.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return a.Start()
}

func (a *Apache) Status() services.ServiceStatus {
	a.mu.Lock()
	defer a.mu.Unlock()

	status := services.StatusStopped
	if a.StatusText != "" {
		status = a.StatusText
	} else if a.Running {
		status = services.StatusRunning
	}

	startedAt := ""
	if !a.StartTime.IsZero() {
		startedAt = a.StartTime.Format(time.RFC3339)
	}

	return services.ServiceStatus{
		Name:      "apache",
		Status:    status,
		Port:      a.ServicePort,
		Version:   a.version,
		PID:       a.ProcessPID,
		Uptime:    a.GetUptime(),
		StartedAt: startedAt,
		Error:     a.LastError,
	}
}

func (a *Apache) Logs(lines int) []string {
	return a.logStore.Get("apache", lines)
}

func (a *Apache) Version() string {
	return a.version
}

func (a *Apache) SetVersion(version string) error {
	installPath := a.paths.ApachePath(version)
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("apache: version %s not installed", version)
	}
	a.version = version
	return a.store.SaveServiceConfig("apache", config.ServiceConfig{
		Name:        "apache",
		Version:     version,
		InstallPath: installPath,
		Port:        a.ServicePort,
		Enabled:     true,
	})
}

func (a *Apache) IsInstalled() bool {
	_, err := a.findHttpd()
	return err == nil
}

func (a *Apache) findHttpd() (string, error) {
	cfg, err := a.store.GetServiceConfig("apache")
	if err == nil && cfg.InstallPath != "" {
		if found := a.findHttpdIn(cfg.InstallPath); found != "" {
			a.version = cfg.Version
			a.ServicePort = cfg.Port
			return found, nil
		}
	}

	installedDir := filepath.Join(a.paths.InstalledPath(), "apache")
	entries, err := os.ReadDir(installedDir)
	if err != nil {
		return "", fmt.Errorf("no apache installation found")
	}

	for _, entry := range entries {
		if entry.IsDir() {
			versionDir := filepath.Join(installedDir, entry.Name())
			if found := a.findHttpdIn(versionDir); found != "" {
				a.version = entry.Name()
				return found, nil
			}
		}
	}

	return "", fmt.Errorf("no apache installation found")
}

func (a *Apache) findHttpdIn(dir string) string {
	// Check common layouts: dir/Apache24/bin/, dir/bin/
	candidates := []string{
		filepath.Join(dir, "Apache24", "bin", "httpd.exe"),
		filepath.Join(dir, "bin", "httpd.exe"),
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
				filepath.Join(dir, e.Name(), "Apache24", "bin", "httpd.exe"),
				filepath.Join(dir, e.Name(), "bin", "httpd.exe"),
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

func (a *Apache) waitForExit() {
	if a.cmd != nil {
		a.cmd.Wait()
	}
	a.mu.Lock()
	a.Running = false
	a.ProcessPID = 0
	a.mu.Unlock()
	a.logStore.Add("apache", "Apache process exited")
}

func (a *Apache) generateConfig(confPath, serverRoot string) error {
	docRoot := a.paths.WwwPath()
	os.MkdirAll(docRoot, 0755)

	phpInfo := a.findPHP()

	sslEnabled := false
	if cfg, err := a.store.GetAppConfig(); err == nil {
		sslEnabled = cfg.SSLEnabled
	}

	data := map[string]interface{}{
		"ServerRoot":   filepath.ToSlash(serverRoot),
		"Listen":       a.ServicePort,
		"DocumentRoot": filepath.ToSlash(docRoot),
		"LogDir":       filepath.ToSlash(a.paths.LogsPath()),
		"ConfDir":      filepath.ToSlash(a.paths.ConfPath("apache")),
		"PHPModule":    phpInfo.ModulePath,
		"PHPIniDir":    phpInfo.IniDir,
		"PHPCgiExe":    phpInfo.CgiExe,
		"PHPCgiDir":    phpInfo.CgiDir,
		"SSLEnabled":   sslEnabled,
	}

	return renderTemplateStr(httpdConfTmpl, confPath, data)
}

type phpInfo struct {
	ModulePath string // path to php8apache2_4.dll (empty if not found)
	IniDir     string // path to php directory for PHPIniDir
	CgiExe     string // path to php-cgi.exe (empty if not found)
	CgiDir     string // directory containing php-cgi.exe (with trailing slash)
}

// findPHP locates the PHP Apache module DLL or php-cgi.exe for the active PHP version.
func (a *Apache) findPHP() phpInfo {
	cfg, err := a.store.GetAppConfig()
	if err != nil || cfg.ActivePHP == "" {
		return phpInfo{}
	}

	phpDir := a.paths.PHPPath(cfg.ActivePHP)
	if _, err := os.Stat(phpDir); os.IsNotExist(err) {
		return phpInfo{}
	}

	info := phpInfo{
		IniDir: filepath.ToSlash(phpDir),
	}

	// Check for php-cgi.exe (always useful as fallback)
	cgiExe := filepath.Join(phpDir, "php-cgi.exe")
	if _, err := os.Stat(cgiExe); err == nil {
		info.CgiExe = filepath.ToSlash(cgiExe)
		info.CgiDir = filepath.ToSlash(phpDir) + "/"
	}

	// Look for php*apache2_4.dll (mod_php — preferred)
	entries, err := os.ReadDir(phpDir)
	if err != nil {
		return info
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() &&
			(name == "php8apache2_4.dll" || name == "php7apache2_4.dll" ||
				name == "php_apache2_4.dll") {
			info.ModulePath = filepath.ToSlash(filepath.Join(phpDir, name))
			return info
		}
	}

	return info
}

func (a *Apache) GetConfPath() string {
	return filepath.Join(a.paths.ConfPath("apache"), "httpd.conf")
}

func (a *Apache) GetVhostDir() string {
	return filepath.Join(a.paths.ConfPath("apache"), "vhosts")
}
