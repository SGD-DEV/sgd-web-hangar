package nginx

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
	"github.com/devour-app/devour/app/phpfcgi"
	"github.com/devour-app/devour/app/portinfo"
	"github.com/devour-app/devour/app/services"
)

// parseBindFailedPort extracts the port number from a nginx error of the form
//
//	nginx: [emerg] bind() to 0.0.0.0:80 failed (10048: ...)
//	nginx: [emerg] bind() to [::]:443 failed (10048: ...)
//
// Returns 0, false if the error wasn't a bind() failure or the port couldn't
// be parsed.
func parseBindFailedPort(errMsg string) (int, bool) {
	// Find the "bind() to <addr>:<port> failed" fragment.
	const marker = "bind() to "
	i := strings.Index(errMsg, marker)
	if i < 0 {
		return 0, false
	}
	tail := errMsg[i+len(marker):]
	// tail looks like "0.0.0.0:80 failed ..." or "[::]:443 failed ..."
	endIdx := strings.Index(tail, " failed")
	if endIdx < 0 {
		return 0, false
	}
	addr := tail[:endIdx]
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

type Nginx struct {
	services.BaseService
	paths    config.Paths
	store    *config.Store
	logStore *logs.Store
	php      *phpfcgi.Pool
	cmd      *exec.Cmd
	mu       sync.Mutex
	version  string
}

func New(paths config.Paths, store *config.Store, logStore *logs.Store, php *phpfcgi.Pool) *Nginx {
	return &Nginx{
		BaseService: services.BaseService{
			ServiceName: "nginx",
			ServicePort: 80,
		},
		paths:    paths,
		store:    store,
		logStore: logStore,
		php:      php,
	}
}

func (n *Nginx) Name() string {
	return "nginx"
}

func (n *Nginx) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.Running {
		return nil
	}

	nginxPath, err := n.findNginx()
	if err != nil {
		return fmt.Errorf("nginx: %w", err)
	}

	// The prefix directory is where nginx resolves relative paths (mime.types, fastcgi_params)
	prefixDir := filepath.Dir(nginxPath)

	// Read port from config
	if cfg, err := n.store.GetAppConfig(); err == nil {
		if cfg.NginxPort > 0 {
			n.ServicePort = cfg.NginxPort
		}
	}

	// Ensure the default welcome page exists in data/www/
	if err := services.EnsureWelcomePage(n.paths.WwwPath(), n.paths.ProjectsPath()); err != nil {
		n.logStore.Add("nginx", fmt.Sprintf("Warning: could not write welcome page: %v", err))
	}

	confPath := filepath.Join(n.paths.ConfPath("nginx"), "nginx.conf")
	// Always regenerate config to ensure PrefixDir and paths are correct
	os.MkdirAll(filepath.Join(n.paths.ConfPath("nginx"), "sites"), 0755)
	if err := n.generateConfig(confPath, prefixDir); err != nil {
		return fmt.Errorf("nginx: generating config: %w", err)
	}

	// Pre-flight: run `nginx -t` to validate the config. If it fails (bad
	// vhost, port already in use, missing file referenced from a directive)
	// the error message from nginx itself is far more useful than the
	// generic "process exited" we'd otherwise log when the actual server
	// dies a fraction of a second after launch.
	testCmd := exec.Command(nginxPath, "-p", prefixDir, "-c", confPath, "-t")
	testCmd.Dir = prefixDir
	services.HideWindow(testCmd)
	if testOutput, testErr := testCmd.CombinedOutput(); testErr != nil {
		errMsg := strings.TrimSpace(string(testOutput))
		// If nginx blamed a bind() failure on a specific port, find who's
		// holding it and append PID+name to the error. The frontend parses
		// the trailing "[port=X pid=Y name=Z]" tag to render a kill button.
		if port, ok := parseBindFailedPort(errMsg); ok {
			if owner, _ := portinfo.OwnerOfPort(port); owner != nil {
				errMsg += fmt.Sprintf(" [port=%d pid=%d name=%s]", owner.Port, owner.PID, owner.Name)
			}
		}
		n.LastError = errMsg
		n.logStore.Add("nginx", "config check failed: "+errMsg)
		return fmt.Errorf("nginx: %s", errMsg)
	}

	if err := n.php.EnsureRunning(); err != nil {
		n.logStore.AddWithLevel("nginx", "PHP workers: "+err.Error(), "warn")
	}

	n.cmd = exec.Command(nginxPath, "-p", prefixDir, "-c", confPath)
	n.cmd.Dir = prefixDir
	services.HideWindow(n.cmd)

	if err := n.cmd.Start(); err != nil {
		n.php.StopAll()
		n.LastError = err.Error()
		return fmt.Errorf("nginx: starting: %w", err)
	}

	n.Running = true
	n.ProcessPID = n.cmd.Process.Pid
	n.StartTime = time.Now()
	n.LastError = ""

	go n.waitForExit()

	n.logStore.Add("nginx", fmt.Sprintf("Nginx started (PID: %d)", n.ProcessPID))
	return nil
}

func (n *Nginx) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.Running || n.cmd == nil || n.cmd.Process == nil {
		n.Running = false
		return nil
	}

	nginxPath, _ := n.findNginx()
	if nginxPath != "" {
		prefixDir := filepath.Dir(nginxPath)
		stopCmd := exec.Command(nginxPath, "-p", prefixDir, "-s", "stop")
		stopCmd.Dir = prefixDir
		services.HideWindow(stopCmd)
		if err := stopCmd.Run(); err != nil {
			if killErr := n.cmd.Process.Kill(); killErr != nil {
				return fmt.Errorf("nginx: stopping: %w", killErr)
			}
		}
	} else {
		if err := n.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("nginx: stopping: %w", err)
		}
	}

	n.php.StopAll()

	n.Running = false
	n.ProcessPID = 0
	n.StartTime = time.Time{}
	n.logStore.Add("nginx", "Nginx stopped")
	return nil
}

func (n *Nginx) Restart() error {
	if err := n.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return n.Start()
}

func (n *Nginx) Status() services.ServiceStatus {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.Running {
		if cfg, err := n.store.GetAppConfig(); err == nil && cfg.NginxPort > 0 {
			n.ServicePort = cfg.NginxPort
		}
	}

	status := services.StatusStopped
	if n.StatusText != "" {
		status = n.StatusText
	} else if n.Running {
		status = services.StatusRunning
	}

	startedAt := ""
	if !n.StartTime.IsZero() {
		startedAt = n.StartTime.Format(time.RFC3339)
	}

	return services.ServiceStatus{
		Name:      "nginx",
		Status:    status,
		Port:      n.ServicePort,
		Version:   n.version,
		PID:       n.ProcessPID,
		Uptime:    n.GetUptime(),
		StartedAt: startedAt,
		Error:     n.LastError,
	}
}

func (n *Nginx) Logs(lines int) []string {
	return n.logStore.Get("nginx", lines)
}

func (n *Nginx) Version() string {
	return n.version
}

func (n *Nginx) SetVersion(version string) error {
	installPath := n.paths.NginxPath(version)
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("nginx: version %s not installed", version)
	}
	n.version = version
	return n.store.SaveServiceConfig("nginx", config.ServiceConfig{
		Name:        "nginx",
		Version:     version,
		InstallPath: installPath,
		Port:        n.ServicePort,
		Enabled:     true,
	})
}

func (n *Nginx) IsInstalled() bool {
	_, err := n.findNginx()
	return err == nil
}

func (n *Nginx) findNginx() (string, error) {
	cfg, err := n.store.GetServiceConfig("nginx")
	if err == nil && cfg.InstallPath != "" {
		if found := n.findNginxIn(cfg.InstallPath); found != "" {
			// The port comes from AppConfig.NginxPort (read in Start); the
			// per-service record may still hold the old 8080 default.
			n.version = cfg.Version
			return found, nil
		}
	}

	installedDir := filepath.Join(n.paths.InstalledPath(), "nginx")
	entries, err := os.ReadDir(installedDir)
	if err != nil {
		return "", fmt.Errorf("no nginx installation found")
	}

	for _, entry := range entries {
		if entry.IsDir() {
			versionDir := filepath.Join(installedDir, entry.Name())
			if found := n.findNginxIn(versionDir); found != "" {
				n.version = entry.Name()
				return found, nil
			}
		}
	}

	return "", fmt.Errorf("no nginx installation found")
}

func (n *Nginx) findNginxIn(dir string) string {
	// Direct: dir/nginx.exe
	candidate := filepath.Join(dir, "nginx.exe")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Nested one level (ZIP extracts to subdir like nginx-1.29.1/)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			candidate = filepath.Join(dir, e.Name(), "nginx.exe")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

func (n *Nginx) waitForExit() {
	startTime := n.StartTime
	if n.cmd != nil {
		n.cmd.Wait()
	}
	n.mu.Lock()
	n.Running = false
	n.StatusText = ""
	n.ProcessPID = 0
	n.mu.Unlock()

	// If nginx exited within ~3s of starting it didn't crash randomly - it
	// failed to bind a port or hit a runtime config error. Surface the tail
	// of the error log so the user can see why instead of just "exited".
	if !startTime.IsZero() && time.Since(startTime) < 3*time.Second {
		errLog := filepath.Join(n.paths.LogsPath(), "nginx_error.log")
		if tail := readLogTail(errLog, 4096); tail != "" {
			n.logStore.Add("nginx", "Nginx exited shortly after start. Last error log entries:\n"+tail)
		} else {
			n.logStore.Add("nginx", "Nginx process exited (no error log available)")
		}
	} else {
		n.logStore.Add("nginx", "Nginx process exited")
	}
}

// readLogTail returns up to maxBytes from the end of path. Used to surface
// a service's own error log entries when the process exits unexpectedly.
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

func (n *Nginx) generateConfig(confPath, prefixDir string) error {
	docRoot := n.paths.WwwPath()
	os.MkdirAll(docRoot, 0755)

	defaultUpstream := ""
	if cfg, err := n.store.GetAppConfig(); err == nil && n.php.HasVersion(cfg.ActivePHP) {
		defaultUpstream = phpfcgi.UpstreamName(cfg.ActivePHP)
	}

	data := map[string]interface{}{
		"WorkerProcesses":   1,
		"WorkerConnections": 1024,
		"Listen":            n.ServicePort,
		"ServerName":        "localhost",
		"DocumentRoot":      filepath.ToSlash(docRoot),
		"LogDir":            filepath.ToSlash(n.paths.LogsPath()),
		"ConfDir":           filepath.ToSlash(n.paths.ConfPath("nginx")),
		"PrefixDir":         filepath.ToSlash(prefixDir),
		"Upstreams":         n.php.Upstreams(),
		"DefaultUpstream":   defaultUpstream,
	}

	return renderTemplateStr(nginxConfTmpl, confPath, data)
}

func (n *Nginx) GetConfPath() string {
	return filepath.Join(n.paths.ConfPath("nginx"), "nginx.conf")
}

func (n *Nginx) GetSitesDir() string {
	return filepath.Join(n.paths.ConfPath("nginx"), "sites")
}

func (n *Nginx) GetPrefixDir() string {
	nginxPath, err := n.findNginx()
	if err != nil {
		return ""
	}
	return filepath.Dir(nginxPath)
}
