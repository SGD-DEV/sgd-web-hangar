package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/devour-app/devour/app/bootstrap"
	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/core"
	"github.com/devour-app/devour/app/dbinspect"
	"github.com/devour-app/devour/app/dns"
	"github.com/devour-app/devour/app/heidisql"
	"github.com/devour-app/devour/app/logs"
	"github.com/devour-app/devour/app/mcp"
	"github.com/devour-app/devour/app/packages"
	"github.com/devour-app/devour/app/php"
	"github.com/devour-app/devour/app/phpfcgi"
	"github.com/devour-app/devour/app/portinfo"
	"github.com/devour-app/devour/app/projects"
	"github.com/devour-app/devour/app/services"
	"github.com/devour-app/devour/app/ssl"
	"github.com/devour-app/devour/app/syspath"
	"github.com/devour-app/devour/app/tunnel"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx            context.Context
	config         *config.Store
	paths          config.Paths
	serviceManager *services.Manager
	phpPool        *phpfcgi.Pool
	tunnel         *tunnel.Manager
	phpManager     *php.Manager
	pathManager    syspath.Manager
	projectManager *projects.Manager
	sslManager     *ssl.Manager
	dnsServer      *dns.Server
	mcpServer      *mcp.Server
	logStore       *logs.Store
	pkgManager     *packages.Manager
	bootstrapMgr   *bootstrap.Manager
	mu             sync.RWMutex
	terminalCmd    *exec.Cmd
	terminalStdin  io.WriteCloser
	// allowQuit becomes true when the user explicitly chose Quit (tray menu
	// item or future File > Quit). The OnBeforeClose hook reads this to
	// distinguish "real exit" from "X button clicked while services run"
	// (the latter hides to tray instead).
	allowQuit bool
	// startupErr records why Startup aborted (almost always: bbolt couldn't
	// acquire an exclusive flock on hangar.db because another Hangar process
	// is still alive in the tray). Surfaced to the UI via GetStartupError so
	// the frontend can show "Hangar is already running, check the tray"
	// instead of a wall of "service manager not initialized" errors.
	startupErr string
}

func NewApp() *App {
	return &App{}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx

	// Delegate to the shared core. The Wails-specific bits stay here:
	// ctx, EventsEmit-backed package emitter, startupErr translation,
	// and the runtime.LogXxx hookup. The actual wiring of managers
	// lives in app/core so the CLI and daemon can reuse it.
	emit := func(event string, data interface{}) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, event, data)
		}
	}
	log := func(level core.Level, format string, args ...interface{}) {
		switch level {
		case core.LevelError:
			runtime.LogErrorf(ctx, "devour: "+format, args...)
		case core.LevelWarn:
			runtime.LogWarningf(ctx, "devour: "+format, args...)
		default:
			runtime.LogInfof(ctx, "devour: "+format, args...)
		}
	}

	c, err := core.Bootstrap(core.DefaultGUIOptions(emit), log)
	if err != nil {
		runtime.LogErrorf(ctx, "devour: bootstrap: %v", err)
		if _, ok := err.(core.ErrAlreadyRunning); ok {
			a.startupErr = "Hangar is already running. Check the system tray (look for the Hangar icon near the clock) and use Quit from its menu, or close it from Task Manager, then try again."
		} else {
			a.startupErr = "Hangar failed to start: " + err.Error()
		}
		return
	}

	// Copy managers into the App fields so the existing 1000+ lines of
	// Wails methods bound to *App keep working unchanged. Treat this
	// as a view onto Core, not a separate ownership.
	a.paths = c.Paths
	a.config = c.Config
	a.logStore = c.Logs
	a.pathManager = c.PathManager
	a.serviceManager = c.ServiceManager
	a.phpPool = c.PHPPool
	a.tunnel = c.Tunnel
	a.phpManager = c.PHPManager
	a.projectManager = c.ProjectManager
	a.sslManager = c.SSLManager
	a.dnsServer = c.DNSServer
	a.mcpServer = c.MCPServer
	a.pkgManager = c.PackageManager
	a.bootstrapMgr = c.BootstrapMgr

	// php.ini is GUI-specific - it depends on Wails being live to push
	// events, and CLI/daemon don't need it pre-warmed for short runs.
	a.ensurePHPIni()

	// Bring back the services that were running before the last shutdown
	// or reboot, then keep an eye on them.
	go a.autostartServices()

	runtime.LogInfo(ctx, "devour: startup complete")
}

// HasRunningServices reports whether at least one registered service is
// currently in StatusRunning. main.go's OnBeforeClose hook uses this to
// decide whether the title-bar X should hide to tray (something running)
// or actually quit (everything stopped).
func (a *App) HasRunningServices() bool {
	if a.serviceManager == nil {
		return false
	}
	for _, st := range a.serviceManager.AllStatuses() {
		if st.Status == services.StatusRunning {
			return true
		}
	}
	return false
}

// AllowQuit lets the tray "Quit Devour" menu item bypass the close-to-tray
// behavior. After this is called, the next OnBeforeClose returns false so
// the window can actually close.
func (a *App) AllowQuit() {
	a.mu.Lock()
	a.allowQuit = true
	a.mu.Unlock()
}

// AllowingQuit reports whether AllowQuit() was called - used by main.go's
// OnBeforeClose to short-circuit the running-services check.
func (a *App) AllowingQuit() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.allowQuit
}

// Ctx returns the Wails context captured in Startup. Returns nil if
// Startup hasn't run yet. The SingleInstanceLock callback in main.go
// uses this and must nil-check: a second instance could theoretically
// fire it before our first instance's Startup completes.
func (a *App) Ctx() context.Context {
	return a.ctx
}

// GetStartupError returns "" if startup succeeded, otherwise a
// user-readable description of what went wrong (typically: another
// Hangar process is still running and holding the config DB lock).
// The frontend polls this on mount and shows a banner instead of the
// blank-everything "service manager not initialized" walls.
func (a *App) GetStartupError() string {
	return a.startupErr
}

func (a *App) Shutdown(ctx context.Context) {
	if a.mcpServer != nil {
		a.mcpServer.Stop()
	}
	if a.dnsServer != nil {
		a.dnsServer.Stop()
	}
	if a.serviceManager != nil {
		a.serviceManager.StopAll()
	}
	if a.phpPool != nil {
		a.phpPool.StopAll()
	}
	if a.config != nil {
		a.config.Close()
	}
	runtime.LogInfo(ctx, "devour: shutdown complete")
}

// --- Service control methods exposed to frontend ---

func (a *App) StartService(name string) error {
	if a.serviceManager == nil {
		return fmt.Errorf("service manager not initialized")
	}

	// Starting a web server while the other one runs means "switch": both
	// use the same port, so stop the other one instead of failing.
	if isWebServer(name) {
		return a.SwitchWebServer(name)
	}
	a.setDesired(name, true)

	// For database services, start asynchronously so the UI gets "starting"
	// status immediately instead of blocking for minutes during initialization.
	if name == "mysql" || name == "postgresql" || name == "mailpit" {
		go func() {
			if err := a.serviceManager.Start(name); err != nil {
				runtime.LogErrorf(a.ctx, "devour: starting %s: %v", name, err)
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "service:error", map[string]string{
						"service": name,
						"error":   err.Error(),
					})
				}
			}
		}()
		return nil
	}

	return a.serviceManager.Start(name)
}

// defaultPHPExtensions are enabled the first time we generate php.ini for a
// freshly installed PHP version. The list is the minimum needed for
// `composer create-project laravel/laravel` to succeed and for the resulting
// app to connect to the databases Devour bundles. Anything outside this list
// stays disabled - users can toggle individually from the Packages UI.
//
// Why each one:
//   openssl, mbstring   - Laravel framework hard requirements
//   fileinfo            - Composer recommended; Laravel file uploads
//   zip                 - Composer extracts downloaded packages
//   curl                - Composer fetches packages over HTTPS; Laravel HTTP client
//   pdo_sqlite          - Laravel's default DB driver since v11
//   pdo_mysql           - connect to the MySQL service Devour ships
//   pdo_pgsql           - connect to the PostgreSQL service Devour ships
//
// Not in the list (intentionally), because they're statically compiled into
// Windows PHP builds and don't need an extension= line:
//   ctype, dom, filter, hash, json, pcre, pdo, session, tokenizer, xml
//
// We don't re-apply on every startup, so users stay in control after the
// initial generation - disabling one of these later isn't undone.
var defaultPHPExtensions = []string{
	"openssl",
	"mbstring",
	"fileinfo",
	"zip",
	"curl",
	"pdo_sqlite",
	"pdo_mysql",
	"pdo_pgsql",
}

// ensurePHPIni creates php.ini from php.ini-production if it doesn't exist for
// the active PHP version, then enables the default extension set.
func (a *App) ensurePHPIni() {
	if a.phpManager == nil {
		return
	}
	versions := a.phpManager.ListInstalled()
	for _, v := range versions {
		phpDir := a.paths.PHPPath(v.Version)
		iniPath := filepath.Join(phpDir, "php.ini")
		if _, err := os.Stat(iniPath); err == nil {
			continue // already exists - don't overwrite user customizations
		}
		// Create from production template
		prodIni := filepath.Join(phpDir, "php.ini-production")
		if _, err := os.Stat(prodIni); err == nil {
			data, err := os.ReadFile(prodIni)
			if err == nil {
				patched := applyDefaultPHPExtensions(data, phpDir)
				if err := os.WriteFile(iniPath, patched, 0644); err == nil {
					runtime.LogInfof(a.ctx, "devour: created php.ini for PHP %s with default extensions", v.Version)
				}
			}
		}
	}
}

// applyDefaultPHPExtensions takes the raw php.ini-production bytes and returns
// a patched copy that:
//   - uncomments `extension_dir = "ext"` (required on Windows for extensions to load)
//   - enables every extension in defaultPHPExtensions, either by uncommenting an
//     existing `;extension=name` / `;extension=php_name.dll` line, or by appending
//     a fresh `extension=name` line if no commented variant exists.
func applyDefaultPHPExtensions(data []byte, phpDir string) []byte {
	lines := strings.Split(string(data), "\n")

	// Track which extensions still need an append at the end.
	needed := make(map[string]bool, len(defaultPHPExtensions))
	for _, ext := range defaultPHPExtensions {
		needed[ext] = true
	}

	extDirSet := false

	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)

		// Uncomment extension_dir. Common forms in php.ini-production:
		//   ; extension_dir = "ext"
		//   ;extension_dir = "ext"
		// Don't touch ./ext (Linux) variants - Windows uses bare "ext".
		if !extDirSet {
			noSemi := strings.TrimSpace(strings.TrimLeft(trimmed, ";"))
			if strings.HasPrefix(noSemi, "extension_dir") && strings.Contains(noSemi, "\"ext\"") {
				lines[i] = "extension_dir = \"ext\""
				extDirSet = true
				continue
			}
		}

		// Match commented-out extension lines for any ext in `needed`.
		// PHP on Windows historically uses `extension=php_curl.dll`; modern PHP
		// (7.2+) accepts the short form `extension=curl`. Both appear in stock
		// php.ini-production - handle both.
		if strings.HasPrefix(trimmed, ";") {
			body := strings.TrimSpace(strings.TrimLeft(trimmed, ";"))
			if !strings.HasPrefix(body, "extension") {
				continue
			}
			eq := strings.Index(body, "=")
			if eq < 0 {
				continue
			}
			val := strings.TrimSpace(body[eq+1:])
			val = strings.Trim(val, "\"'")
			val = strings.TrimSuffix(val, ".dll")
			val = strings.TrimPrefix(val, "php_")
			if needed[val] {
				lines[i] = "extension=" + val
				delete(needed, val)
			}
		} else if strings.HasPrefix(trimmed, "extension") {
			// Already enabled - mark satisfied so we don't double-add.
			eq := strings.Index(trimmed, "=")
			if eq > 0 {
				val := strings.TrimSpace(trimmed[eq+1:])
				val = strings.Trim(val, "\"'")
				val = strings.TrimSuffix(val, ".dll")
				val = strings.TrimPrefix(val, "php_")
				delete(needed, val)
			}
		}
	}

	// Append any extensions we didn't find a commented variant for. Only append
	// ones whose .dll actually exists in ext/, to avoid PHP warning on startup
	// about a missing extension.
	if len(needed) > 0 {
		extDir := filepath.Join(phpDir, "ext")
		var appended []string
		for ext := range needed {
			dll := filepath.Join(extDir, "php_"+ext+".dll")
			if _, err := os.Stat(dll); err == nil {
				appended = append(appended, "extension="+ext)
			}
		}
		if len(appended) > 0 {
			lines = append(lines, "", "; --- Devour default extensions ---")
			lines = append(lines, appended...)
		}
	}

	return []byte(strings.Join(lines, "\n"))
}

func (a *App) StopService(name string) error {
	if a.serviceManager == nil {
		return fmt.Errorf("service manager not initialized")
	}
	a.setDesired(name, false)
	return a.serviceManager.Stop(name)
}

func (a *App) RestartService(name string) error {
	if a.serviceManager == nil {
		return fmt.Errorf("service manager not initialized")
	}
	return a.serviceManager.Restart(name)
}

func (a *App) GetServiceStatus(name string) (services.ServiceStatus, error) {
	if a.serviceManager == nil {
		return services.ServiceStatus{}, fmt.Errorf("service manager not initialized")
	}
	return a.serviceManager.Status(name)
}

func (a *App) GetAllServiceStatuses() map[string]services.ServiceStatus {
	if a.serviceManager == nil {
		return map[string]services.ServiceStatus{}
	}
	return a.serviceManager.AllStatuses()
}

func (a *App) StartAllServices() error {
	if a.serviceManager == nil {
		return fmt.Errorf("service manager not initialized")
	}
	return a.serviceManager.StartAll()
}

func (a *App) StopAllServices() error {
	if a.serviceManager == nil {
		return nil
	}
	for _, name := range a.serviceManager.Names() {
		a.setDesired(name, false)
	}
	a.serviceManager.StopAll()
	return nil
}

// --- PHP methods ---

func (a *App) GetInstalledPHPVersions() []php.VersionInfo {
	return a.phpManager.ListInstalled()
}

func (a *App) GetActivePHPVersion() string {
	return a.phpManager.ActiveVersion()
}

func (a *App) SwitchPHP(version string) error {
	return a.phpManager.SwitchGlobal(version)
}

func (a *App) SwitchPHPForProject(version, projectName string) error {
	return a.phpManager.SwitchForProject(version, projectName)
}

func (a *App) GetPHPIniSettings(version string) (php.IniSettings, error) {
	return a.phpManager.GetIniSettings(version)
}

func (a *App) UpdatePHPIniSettings(version string, settings php.IniSettings) error {
	return a.phpManager.UpdateIniSettings(version, settings)
}

// --- Database query + inspection (powers MCP run_sql / inspect_database) ---
//
// Local-only: the MCP server is bound to 127.0.0.1, and the DSN is built
// from Hangar's own bundled MySQL/Postgres credentials (root with no
// password by default). Don't expose any of this beyond localhost.

// RunSQL executes a query against the named local DB. dbType is "mysql" or
// "postgresql". database is optional; for MySQL it goes in the DSN's
// path, for Postgres in dbname=. Returns columns+rows for SELECTs,
// rows_affected for DML, capped at 500 rows.
func (a *App) RunSQL(dbType, database, sqlText string) (*dbinspect.QueryResult, error) {
	dsn, err := a.dbDSN(dbType, database)
	if err != nil {
		return nil, err
	}
	return dbinspect.RunQuery(dbType, dsn, sqlText)
}

// InspectDatabase dumps the schema (tables, columns, indexes, FKs, triggers)
// for the named DB. Returns a structured Schema and a ready-to-execute audit
// prompt the AI agent can run against the schema in its own context.
func (a *App) InspectDatabase(dbType, database, projectContext string) (map[string]interface{}, error) {
	dsn, err := a.dbDSN(dbType, database)
	if err != nil {
		return nil, err
	}
	schema, err := dbinspect.Inspect(dbType, dsn, database)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"schema":       schema,
		"audit_prompt": dbinspect.BuildAuditPrompt(schema, projectContext),
	}, nil
}

// dbDSN constructs a driver DSN for one of the local DBs Hangar manages.
// Reads the configured port from app config; assumes user=root (mysql) /
// user=postgres (pg) with no password (matches the init-time grants we set
// up when bootstrapping each service).
func (a *App) dbDSN(dbType, database string) (string, error) {
	if a.config == nil {
		return "", fmt.Errorf("config not initialized")
	}
	cfg, _ := a.config.GetAppConfig()
	switch strings.ToLower(dbType) {
	case "mysql", "mariadb":
		port := cfg.MySQLPort
		if port == 0 {
			port = 3306
		}
		return dbinspect.BuildDSN("mysql", "127.0.0.1", port, "root", "", database), nil
	case "postgresql", "postgres", "pg":
		port := cfg.PostgreSQLPort
		if port == 0 {
			port = 5432
		}
		return dbinspect.BuildDSN("postgres", "127.0.0.1", port, "postgres", "", database), nil
	}
	return "", fmt.Errorf("unknown db type %q", dbType)
}

// --- Custom packages (user-added registry entries) ---

// DetectPackageURL takes a download URL and returns a best-effort guess of
// what kind of package it is, what version, and a suggested install dir.
// The frontend prefills the "Add Package URL" form with this so users don't
// have to type name/version/category for known hosts (windows.php.net,
// dev.mysql.com, github releases, etc.).
func (a *App) DetectPackageURL(rawURL string) packages.DetectedPackage {
	return packages.URLDetect(rawURL)
}

// AddCustomPackage persists a user-added registry entry. After this call
// the package shows up in GetPackages alongside the built-ins and can be
// installed normally.
func (a *App) AddCustomPackage(p packages.PackageEntry) error {
	if a.pkgManager == nil {
		return fmt.Errorf("package manager not initialized")
	}
	return a.pkgManager.AddCustomPackage(p)
}

// RemoveCustomPackage drops a user-added entry from the registry. Does not
// delete the on-disk install (use RemovePackage for that).
func (a *App) RemoveCustomPackage(name, version string) error {
	if a.pkgManager == nil {
		return fmt.Errorf("package manager not initialized")
	}
	return a.pkgManager.RemoveCustomPackage(name, version)
}

func (a *App) ListCustomPackages() []packages.PackageEntry {
	if a.pkgManager == nil {
		return nil
	}
	return a.pkgManager.ListCustomPackages()
}

// --- Port owner / kill helpers ---
//
// When a service start fails because something else is on the port (the
// most common failure - Apache/Nginx fighting over 80, an old MySQL still
// listening on 3306) the user wants two things: who, and a kill button.
// These two methods power that flow in the frontend.

func (a *App) GetPortOwner(port int) *portinfo.Owner {
	owner, _ := portinfo.OwnerOfPort(port)
	return owner
}

func (a *App) KillProcessByPID(pid int) error {
	return portinfo.Kill(pid)
}

// --- Bootstrap / first-run wizard ---
//
// IsFirstRun returns true the first time Devour starts on a machine (no
// runtimes installed yet). The frontend uses this to decide whether to
// show the welcome wizard.

func (a *App) IsFirstRun() bool {
	if a.bootstrapMgr == nil {
		return false
	}
	return a.bootstrapMgr.IsFirstRun()
}

// GetDefaultBundle returns the curated list of packages we recommend
// installing on first run (PHP, Apache, MySQL, etc.).
func (a *App) GetDefaultBundle() []bootstrap.BundleItem {
	return bootstrap.DefaultBundle()
}

// RunBootstrap installs the supplied bundle items, emitting
// "bootstrap:progress" events. Pass nil/empty for the full default bundle.
func (a *App) RunBootstrap(items []bootstrap.BundleItem) error {
	if a.bootstrapMgr == nil {
		return fmt.Errorf("bootstrap manager not initialized")
	}
	if len(items) == 0 {
		items = bootstrap.DefaultBundle()
	}
	return a.bootstrapMgr.RunBootstrap(items)
}

func (a *App) GetBootstrapStatus() []bootstrap.BundleStatus {
	if a.bootstrapMgr == nil {
		return nil
	}
	return a.bootstrapMgr.Status()
}

// --- System PATH manager ---
//
// These power the "System PATH" page. The pathManager edits the user's
// HKCU\Environment PATH entry and broadcasts WM_SETTINGCHANGE so new
// terminals see the change immediately. No admin required.

func (a *App) GetSystemPathEntries() ([]syspath.Entry, error) {
	if a.pathManager == nil {
		return nil, fmt.Errorf("syspath manager not initialized")
	}
	return a.pathManager.List()
}

func (a *App) GetSystemPathRuntimes() ([]syspath.RuntimeInfo, error) {
	if a.pathManager == nil {
		return nil, fmt.Errorf("syspath manager not initialized")
	}
	return a.pathManager.GetRuntimes()
}

func (a *App) SetActiveRuntimeOnPath(runtime, version string) error {
	if a.pathManager == nil {
		return fmt.Errorf("syspath manager not initialized")
	}
	return a.pathManager.SetActiveRuntime(runtime, version)
}

func (a *App) AddSystemPathEntry(path string) error {
	if a.pathManager == nil {
		return fmt.Errorf("syspath manager not initialized")
	}
	return a.pathManager.Add(path, syspath.ScopeUser)
}

func (a *App) RemoveSystemPathEntry(path string, scope string) error {
	if a.pathManager == nil {
		return fmt.Errorf("syspath manager not initialized")
	}
	s := syspath.ScopeUser
	if scope == "system" {
		s = syspath.ScopeSystem
	}
	return a.pathManager.Remove(path, s)
}

// --- Project methods ---

func (a *App) GetProjects() []projects.Project {
	return a.projectManager.List()
}

func (a *App) CreateProject(name, path, domain string) (projects.Project, error) {
	return a.projectManager.Create(name, path, domain)
}

// CreateProjectWithOptions is the rich-form variant. Frontend uses this for
// proxy projects (Framework="proxy", ProxyTarget="http://localhost:8000")
// where there's no on-disk source directory.
func (a *App) CreateProjectWithOptions(opts projects.CreateOptions) (projects.Project, error) {
	if a.projectManager == nil {
		return projects.Project{}, fmt.Errorf("project manager not initialized")
	}
	return a.projectManager.CreateWithOptions(opts)
}

// PickProjectDirectory shows a native folder-picker dialog so users don't
// have to type a path. Returns the selected absolute path, or "" if the
// user cancelled. Wails' OpenDirectoryDialog is platform-aware (uses the
// shell file picker on each OS).
func (a *App) PickProjectDirectory() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app not started")
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                      "Select project folder",
		CanCreateDirectories:       true,
		ResolvesAliases:            true,
		TreatPackagesAsDirectories: false,
	})
}

// UpdateProject persists arbitrary changes to a project record. Frontend
// uses this to set web_server / ssl_enabled / document_root after create.
// Re-runs linkProject so vhosts and the hosts entry reflect the new values.
func (a *App) UpdateProject(name string, project projects.Project) error {
	if a.projectManager == nil {
		return fmt.Errorf("project manager not initialized")
	}
	if err := a.projectManager.Update(name, project); err != nil {
		return err
	}
	// Regenerate vhost configs with the new web_server/ssl/doc_root values.
	if err := a.projectManager.EnsureProjectsReady(); err != nil {
		runtime.LogWarningf(a.ctx, "devour: re-linking projects after update: %v", err)
	}
	return nil
}

func (a *App) DeleteProject(name string) error {
	return a.projectManager.Delete(name)
}

func (a *App) ScanProjects() ([]projects.Project, error) {
	return a.projectManager.Scan()
}

// --- SSL methods ---

func (a *App) InstallRootCA() error {
	return a.sslManager.InstallCA()
}

func (a *App) GenerateSSLCert(domain string) error {
	return a.sslManager.GenerateCert(domain)
}

func (a *App) GetSSLCerts() []ssl.CertInfo {
	return a.sslManager.ListCerts()
}

// --- DNS methods ---

func (a *App) StartDNS() error {
	return a.dnsServer.Start()
}

func (a *App) StopDNS() error {
	return a.dnsServer.Stop()
}

// --- MCP methods ---

func (a *App) StartMCP() error {
	return a.mcpServer.Start()
}

func (a *App) StopMCP() error {
	a.mcpServer.Stop()
	return nil
}

// --- Log methods ---

func (a *App) GetServiceLogs(serviceName string, lines int) []string {
	return a.logStore.Get(serviceName, lines)
}

// --- Config methods ---

func (a *App) GetConfig() (config.AppConfig, error) {
	if a.config == nil {
		return config.AppConfig{}, fmt.Errorf("config not initialized yet")
	}
	return a.config.GetAppConfig()
}

func (a *App) UpdateConfig(cfg config.AppConfig) error {
	// State Hangar manages itself must not be overwritten by a Settings
	// page that was opened before a service was started or switched.
	if cur, err := a.config.GetAppConfig(); err == nil {
		cfg.DesiredServices = cur.DesiredServices
		cfg.ActiveWebServer = cur.ActiveWebServer
		cfg.SchemaVersion = cur.SchemaVersion
		cfg.ActivePHP = cur.ActivePHP
	}
	if cfg.PHPWorkers < 1 || cfg.PHPWorkers > phpfcgi.MaxWorkers {
		cfg.PHPWorkers = phpfcgi.DefaultWorkers
	}
	if cfg.ProjectsRoot != "" {
		if info, err := os.Stat(cfg.ProjectsRoot); err != nil || !info.IsDir() {
			return fmt.Errorf("projects root %s does not exist", cfg.ProjectsRoot)
		}
	}
	return a.config.SaveAppConfig(cfg)
}

func (a *App) GetPaths() map[string]string {
	return map[string]string{
		"base":      a.paths.BasePath(),
		"data":      a.paths.DataPath(),
		"installed": a.paths.InstalledPath(),
		"ssl":       a.paths.SSLPath(),
		"logs":      a.paths.LogsPath(),
		"www":       a.paths.WwwPath(),
		"projects":  a.paths.ProjectsPath(),
	}
}

func (a *App) GetProjectsRoot() string {
	if a.projectManager != nil {
		return a.projectManager.GetProjectsRoot()
	}
	return a.paths.ProjectsPath()
}

// --- Window control methods ---

func (a *App) MinimiseWindow() {
	runtime.WindowMinimise(a.ctx)
}

func (a *App) MaximiseWindow() {
	runtime.WindowToggleMaximise(a.ctx)
}

func (a *App) CloseWindow() {
	runtime.Quit(a.ctx)
}

// --- Package Manager methods ---

func (a *App) GetPackageCategories() []packages.CategoryInfo {
	return a.pkgManager.GetCategories()
}

func (a *App) GetPackages(category string) []packages.PackageInfo {
	return a.pkgManager.GetPackages(category)
}

func (a *App) InstallPackage(name, version string) error {
	return a.pkgManager.InstallPackage(name, version)
}

func (a *App) RemovePackage(name, version string) error {
	return a.pkgManager.RemovePackage(name, version)
}

func (a *App) ActivatePackage(name, version string) error {
	return a.pkgManager.ActivatePackage(name, version)
}

func (a *App) GetDownloadProgress(name, version string) *packages.DownloadProgress {
	return a.pkgManager.GetDownloadProgress(name, version)
}

func (a *App) GetAllDownloadProgress() map[string]*packages.DownloadProgress {
	return a.pkgManager.GetAllProgress()
}

// --- Config file methods ---

// postgresConfigPath returns the path to one of postgres' in-data-dir config
// files (postgresql.conf, pg_hba.conf, pg_ident.conf). Postgres always reads
// these from the data directory it was initialized into - nothing in
// Devour's conf/postgresql/ tree is ever consulted by the running daemon.
func (a *App) postgresConfigPath(relativePath string) string {
	cfg, _ := a.config.GetServiceConfig("postgresql")
	if cfg.DataDir != "" {
		return filepath.Join(cfg.DataDir, relativePath)
	}
	// Fallback: derive from the active version. Mirrors postgres.go::getDataDir.
	version := cfg.Version
	if version == "" {
		version = "default"
	}
	return filepath.Join(a.paths.DataPath(), "pg-data", version, relativePath)
}

// ReadConfigFile reads a config file by service name and relative path
func (a *App) ReadConfigFile(service, relativePath string) (string, error) {
	var fullPath string
	switch {
	case service == "apache" && relativePath == "httpd.conf":
		fullPath = filepath.Join(a.paths.ConfPath("apache"), "httpd.conf")
	case service == "nginx" && relativePath == "nginx.conf":
		fullPath = filepath.Join(a.paths.ConfPath("nginx"), "nginx.conf")
	case service == "mysql" && relativePath == "my.ini":
		fullPath = filepath.Join(a.paths.ConfPath("mysql"), "my.ini")
	case service == "postgresql" && (relativePath == "postgresql.conf" || relativePath == "pg_hba.conf"):
		// Postgres keeps its config files INSIDE the data directory, not in
		// Devour's conf/ tree. Resolve to the active version's pg-data dir.
		fullPath = a.postgresConfigPath(relativePath)
	case service == "php":
		version := a.phpManager.ActiveVersion()
		if version == "" {
			return "", fmt.Errorf("no active PHP version")
		}
		fullPath = filepath.Join(a.paths.PHPPath(version), relativePath)
	default:
		// Allow reading vhost/site configs
		fullPath = filepath.Join(a.paths.ConfPath(service), relativePath)
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", fullPath, err)
	}
	return string(data), nil
}

// WriteConfigFile writes content to a config file
func (a *App) WriteConfigFile(service, relativePath, content string) error {
	var fullPath string
	switch {
	case service == "apache" && relativePath == "httpd.conf":
		fullPath = filepath.Join(a.paths.ConfPath("apache"), "httpd.conf")
	case service == "nginx" && relativePath == "nginx.conf":
		fullPath = filepath.Join(a.paths.ConfPath("nginx"), "nginx.conf")
	case service == "mysql" && relativePath == "my.ini":
		fullPath = filepath.Join(a.paths.ConfPath("mysql"), "my.ini")
	case service == "postgresql" && (relativePath == "postgresql.conf" || relativePath == "pg_hba.conf"):
		// Postgres keeps its config files INSIDE the data directory, not in
		// Devour's conf/ tree. Resolve to the active version's pg-data dir.
		fullPath = a.postgresConfigPath(relativePath)
	case service == "php":
		version := a.phpManager.ActiveVersion()
		if version == "" {
			return fmt.Errorf("no active PHP version")
		}
		fullPath = filepath.Join(a.paths.PHPPath(version), relativePath)
	default:
		fullPath = filepath.Join(a.paths.ConfPath(service), relativePath)
	}

	return os.WriteFile(fullPath, []byte(content), 0644)
}

// GetConfigFileList returns available config files for a service
func (a *App) GetConfigFileList(service string) []map[string]string {
	var files []map[string]string

	switch service {
	case "apache":
		confDir := a.paths.ConfPath("apache")
		files = append(files, map[string]string{"name": "httpd.conf", "path": "httpd.conf"})
		// List vhost files
		vhostDir := filepath.Join(confDir, "vhosts")
		if entries, err := os.ReadDir(vhostDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".conf") {
					files = append(files, map[string]string{
						"name": "vhosts/" + e.Name(),
						"path": "vhosts/" + e.Name(),
					})
				}
			}
		}
	case "nginx":
		confDir := a.paths.ConfPath("nginx")
		files = append(files, map[string]string{"name": "nginx.conf", "path": "nginx.conf"})
		sitesDir := filepath.Join(confDir, "sites")
		if entries, err := os.ReadDir(sitesDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".conf") {
					files = append(files, map[string]string{
						"name": "sites/" + e.Name(),
						"path": "sites/" + e.Name(),
					})
				}
			}
		}
	case "mysql":
		files = append(files, map[string]string{"name": "my.ini", "path": "my.ini"})
	case "postgresql":
		files = append(files, map[string]string{"name": "postgresql.conf", "path": "postgresql.conf"})
	}

	return files
}

// --- PHP extension methods ---

func (a *App) GetPHPExtensions(version string) []php.ExtensionInfo {
	return a.phpManager.GetExtensions(version)
}

func (a *App) TogglePHPExtension(version, extName string, enable bool) error {
	return a.phpManager.ToggleExtension(version, extName, enable)
}

// --- Per-project PHP version ---

func (a *App) UpdateProjectPHP(projectName, phpVersion string) error {
	// Update in config
	if err := a.phpManager.SwitchForProject(phpVersion, projectName); err != nil {
		return err
	}
	// Update the project record
	project, err := a.projectManager.Get(projectName)
	if err != nil {
		return err
	}
	project.PHPVersion = phpVersion
	if err := a.projectManager.Update(projectName, project); err != nil {
		return err
	}
	// Regenerate vhost configs with new PHP CGI path
	if a.projectManager != nil {
		a.projectManager.EnsureProjectsReady()
	}
	return nil
}

// --- Terminal ---

// buildEnv returns an []string environment with PHP, MySQL, Node, Composer in PATH
func (a *App) buildEnv() []string {
	extraPaths := []string{}

	// Add active PHP to PATH
	if a.phpManager != nil {
		activeVer := a.phpManager.ActiveVersion()
		if activeVer != "" {
			extraPaths = append(extraPaths, a.paths.PHPPath(activeVer))
		}
	}

	// Add MySQL bin to PATH
	mysqlInstalled := filepath.Join(a.paths.InstalledPath(), "mysql")
	if entries, err := os.ReadDir(mysqlInstalled); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				binDir := filepath.Join(mysqlInstalled, e.Name(), "bin")
				if _, err := os.Stat(filepath.Join(binDir, "mysql.exe")); err == nil {
					extraPaths = append(extraPaths, binDir)
					break
				}
				if nested, err := os.ReadDir(filepath.Join(mysqlInstalled, e.Name())); err == nil {
					for _, n := range nested {
						if n.IsDir() {
							nestedBin := filepath.Join(mysqlInstalled, e.Name(), n.Name(), "bin")
							if _, err := os.Stat(filepath.Join(nestedBin, "mysql.exe")); err == nil {
								extraPaths = append(extraPaths, nestedBin)
								break
							}
						}
					}
				}
			}
		}
	}

	// Add Node.js to PATH. The user's choice of which Node version is on
	// PATH is managed by syspath (which writes HKCU\Environment), so for
	// in-Devour terminal commands we just need *some* node available.
	// First installed wins; user can override via the System PATH page.
	nodeInstalled := filepath.Join(a.paths.InstalledPath(), "node")
	if entries, err := os.ReadDir(nodeInstalled); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			versionDir := filepath.Join(nodeInstalled, e.Name())
			// Direct: installed/node/<version>/node.exe
			if _, err := os.Stat(filepath.Join(versionDir, "node.exe")); err == nil {
				extraPaths = append(extraPaths, versionDir)
				break
			}
			// Nested: installed/node/<version>/node-vX.Y-win-x64/node.exe
			if nested, err := os.ReadDir(versionDir); err == nil {
				for _, n := range nested {
					if n.IsDir() {
						sub := filepath.Join(versionDir, n.Name())
						if _, err := os.Stat(filepath.Join(sub, "node.exe")); err == nil {
							extraPaths = append(extraPaths, sub)
							break
						}
					}
				}
			}
		}
	}

	// Add Composer to PATH. composer.phar isn't directly executable on Windows,
	// so write a composer.bat shim next to it that invokes "php composer.phar %*".
	// Lazy + idempotent: only writes when missing.
	composerDir := filepath.Join(a.paths.InstalledPath(), "composer")
	composerPhar := filepath.Join(composerDir, "composer.phar")
	if _, err := os.Stat(composerPhar); err == nil {
		composerBat := filepath.Join(composerDir, "composer.bat")
		if _, err := os.Stat(composerBat); os.IsNotExist(err) {
			shim := "@echo off\r\nphp \"" + composerPhar + "\" %*\r\n"
			_ = os.WriteFile(composerBat, []byte(shim), 0644)
		}
		extraPaths = append(extraPaths, composerDir)
	}

	env := os.Environ()
	existingPath := os.Getenv("PATH")
	newPath := strings.Join(extraPaths, ";") + ";" + existingPath
	for i, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			env[i] = "PATH=" + newPath
			break
		}
	}
	return env
}

// findComposer returns the path to composer.phar or composer.bat, empty if not found
func (a *App) findComposer() string {
	composerDir := filepath.Join(a.paths.InstalledPath(), "composer")
	phar := filepath.Join(composerDir, "composer.phar")
	if _, err := os.Stat(phar); err == nil {
		return phar
	}
	// Check if composer is in system PATH
	if p, err := exec.LookPath("composer"); err == nil {
		return p
	}
	if p, err := exec.LookPath("composer.bat"); err == nil {
		return p
	}
	return ""
}

// RunTerminalCommand executes a command in the Devour environment with PHP, MySQL, etc. in PATH.
// Uses streaming: output is sent via "terminal:output" events, and the method returns when done.
func (a *App) RunTerminalCommand(command string) map[string]string {
	a.mu.Lock()
	// Cancel any previously running terminal command
	if a.terminalCmd != nil && a.terminalCmd.Process != nil {
		a.terminalCmd.Process.Kill()
		a.terminalCmd = nil
	}
	a.terminalStdin = nil
	a.mu.Unlock()

	cmd := exec.Command("cmd.exe")
	cmd.Env = a.buildEnv()
	cmd.Dir = a.paths.ProjectsPath()
	services.HideWindow(cmd)
	// Set CmdLine after HideWindow so we preserve HideWindow flags
	// but bypass Go's automatic argument quoting for proper quote handling
	cmd.SysProcAttr.CmdLine = `cmd.exe /C ` + command

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return map[string]string{"output": "", "error": "failed to create stdout pipe: " + err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return map[string]string{"output": "", "error": "failed to create stderr pipe: " + err.Error()}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return map[string]string{"output": "", "error": "failed to create stdin pipe: " + err.Error()}
	}

	a.mu.Lock()
	a.terminalCmd = cmd
	a.terminalStdin = stdin
	a.mu.Unlock()

	if err := cmd.Start(); err != nil {
		a.mu.Lock()
		a.terminalCmd = nil
		a.terminalStdin = nil
		a.mu.Unlock()
		return map[string]string{"output": "", "error": err.Error()}
	}

	// Stream stdout and stderr to frontend via events
	var allOutput strings.Builder
	var allError strings.Builder
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				allOutput.WriteString(chunk)
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "terminal:output", map[string]string{"type": "stdout", "data": chunk})
				}
			}
			if readErr != nil {
				break
			}
		}
	}()
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := stderr.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				allError.WriteString(chunk)
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "terminal:output", map[string]string{"type": "stderr", "data": chunk})
				}
			}
			if readErr != nil {
				break
			}
		}
	}()

	wg.Wait()
	cmdErr := cmd.Wait()

	a.mu.Lock()
	a.terminalCmd = nil
	a.terminalStdin = nil
	a.mu.Unlock()

	// Emit done event
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "terminal:done", map[string]string{})
	}

	result := map[string]string{
		"output": allOutput.String(),
		"error":  allError.String(),
	}
	if cmdErr != nil && result["error"] == "" {
		result["error"] = cmdErr.Error()
	}
	return result
}

// SendTerminalInput sends input to the running terminal command's stdin
func (a *App) SendTerminalInput(input string) error {
	a.mu.RLock()
	stdin := a.terminalStdin
	a.mu.RUnlock()

	if stdin == nil {
		return fmt.Errorf("no running terminal command")
	}
	_, err := fmt.Fprintln(stdin, input)
	return err
}

// CancelTerminalCommand kills the running terminal command
func (a *App) CancelTerminalCommand() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.terminalCmd != nil && a.terminalCmd.Process != nil {
		err := a.terminalCmd.Process.Kill()
		a.terminalCmd = nil
		a.terminalStdin = nil
		return err
	}
	return nil
}

// CreateProjectWithFramework creates a new project using a framework scaffolding command.
// framework: "laravel", "symfony", "wordpress", "plain"
// version: optional version constraint (e.g. "11.*" for Laravel 11)
// name: project folder name (lowercase, no spaces)
// Returns progress messages via events; final result returned.
func (a *App) CreateProjectWithFramework(framework, name, version string) map[string]string {
	result := map[string]string{"output": "", "error": ""}

	if name == "" {
		result["error"] = "Project name is required"
		return result
	}
	name = strings.ToLower(strings.ReplaceAll(name, " ", "-"))

	projectsRoot := a.paths.ProjectsPath()
	projectPath := filepath.Join(projectsRoot, name)

	// Check if project already exists
	if _, err := os.Stat(projectPath); err == nil {
		result["error"] = fmt.Sprintf("Directory %s already exists", name)
		return result
	}

	os.MkdirAll(projectsRoot, 0755)
	env := a.buildEnv()

	var cmd *exec.Cmd

	switch strings.ToLower(framework) {
	case "laravel":
		composer := a.findComposer()
		if composer == "" {
			result["error"] = "Composer not found. Install it from the Packages page."
			return result
		}
		phpExe := ""
		if a.phpManager != nil {
			v := a.phpManager.ActiveVersion()
			if v != "" {
				phpExe = filepath.Join(a.paths.PHPPath(v), "php.exe")
			}
		}
		pkg := "laravel/laravel"
		if version != "" {
			pkg = pkg + ":" + version
		}
		if phpExe != "" && strings.HasSuffix(composer, ".phar") {
			cmd = exec.Command(phpExe, composer, "create-project", "--prefer-dist", pkg, name)
		} else {
			cmd = exec.Command("composer", "create-project", "--prefer-dist", pkg, name)
		}

	case "symfony":
		composer := a.findComposer()
		if composer == "" {
			result["error"] = "Composer not found. Install it from the Packages page."
			return result
		}
		phpExe := ""
		if a.phpManager != nil {
			v := a.phpManager.ActiveVersion()
			if v != "" {
				phpExe = filepath.Join(a.paths.PHPPath(v), "php.exe")
			}
		}
		pkg := "symfony/skeleton"
		if version != "" {
			pkg = pkg + ":" + version
		}
		if phpExe != "" && strings.HasSuffix(composer, ".phar") {
			cmd = exec.Command(phpExe, composer, "create-project", "--prefer-dist", pkg, name)
		} else {
			cmd = exec.Command("composer", "create-project", "--prefer-dist", pkg, name)
		}

	case "wordpress":
		// For WordPress, we download and extract. Use WP-CLI if available, otherwise basic download.
		// Simple approach: use composer create-project for bedrock, or just create dir with index.php
		os.MkdirAll(projectPath, 0755)
		// Create a placeholder — user should download WordPress manually or we can use a wp-cli
		indexContent := `<?php
// WordPress installation
// Download WordPress from https://wordpress.org/download/
// and extract it into this directory.
echo "<h1>WordPress - Placeholder</h1>";
echo "<p>Download WordPress from <a href='https://wordpress.org/download/'>wordpress.org</a> and extract here.</p>";
`
		os.WriteFile(filepath.Join(projectPath, "index.php"), []byte(indexContent), 0644)
		os.WriteFile(filepath.Join(projectPath, "wp-config-sample.php"), []byte("<?php // wp-config sample\n"), 0644)
		result["output"] = "WordPress project directory created. Download WordPress and extract into: " + projectPath
		// Register the project
		a.projectManager.Create(name, projectPath, "")
		a.projectManager.Scan()
		return result

	case "plain":
		os.MkdirAll(projectPath, 0755)
		indexContent := `<?php
phpinfo();
`
		os.WriteFile(filepath.Join(projectPath, "index.php"), []byte(indexContent), 0644)
		result["output"] = "Plain PHP project created at: " + projectPath
		a.projectManager.Create(name, projectPath, "")
		a.projectManager.Scan()
		return result

	default:
		result["error"] = fmt.Sprintf("Unknown framework: %s", framework)
		return result
	}

	// Execute composer command for Laravel/Symfony
	cmd.Dir = projectsRoot
	cmd.Env = env
	services.HideWindow(cmd)

	runtime.LogInfo(a.ctx, fmt.Sprintf("devour: creating %s project '%s'...", framework, name))
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "project:creating", map[string]string{
			"name":      name,
			"framework": framework,
			"status":    "running",
		})
	}

	output, err := cmd.CombinedOutput()
	result["output"] = string(output)

	if err != nil {
		result["error"] = err.Error()
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "project:creating", map[string]string{
				"name":   name,
				"status": "error",
				"error":  err.Error(),
			})
		}
		return result
	}

	// Register project and re-scan (detector will find framework and set DocumentRoot)
	a.projectManager.Create(name, projectPath, "")
	a.projectManager.Scan()

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "project:creating", map[string]string{
			"name":   name,
			"status": "done",
		})
	}

	return result
}

// GetTerminalEnvInfo returns info about available tools in the terminal PATH
func (a *App) GetTerminalEnvInfo() map[string]string {
	info := map[string]string{
		"cwd": a.paths.ProjectsPath(),
	}
	if a.phpManager != nil {
		info["php"] = a.phpManager.ActiveVersion()
	}
	return info
}

// OpenExternalTerminal launches a real OS terminal (Windows Terminal if
// present, falling back to cmd.exe) with Hangar's PATH already set up.
// Interactive programs like psql, mysql, npm-init, php artisan tinker need
// a real PTY which our in-app terminal can't provide - sending them to a
// proper terminal is the pragmatic fix.
//
// cwd is the working directory the new terminal should open in. Empty
// means default to the projects root.
func (a *App) OpenExternalTerminal(cwd string) error {
	if cwd == "" {
		cwd = a.paths.ProjectsPath()
	}

	// Resolve the PATH-augmented environment so wt.exe/cmd.exe inherit
	// php / mysql / psql / composer / node etc. without the user editing
	// system env vars.
	env := a.buildEnv()

	// Prefer Windows Terminal (wt.exe) - tabs, decent rendering, modern.
	// Fall back to cmd.exe which is universal but uglier.
	wt, err := exec.LookPath("wt.exe")
	var cmd *exec.Cmd
	if err == nil {
		// `wt -d <dir>` opens a new tab with that working dir. We give it
		// cmd.exe explicitly so the user's default profile (which may be
		// PowerShell) doesn't override the augmented env we pass.
		cmd = exec.Command(wt, "-d", cwd, "cmd.exe")
	} else {
		// cmd.Dir below sets the child's working directory, so we don't
		// need an extra `cd /d` (which broke on paths with spaces because
		// the whole "cd /d <path>" was passed as a single /K argument and
		// cmd.exe split the path on the first space).
		cmd = exec.Command("cmd.exe", "/K")
	}
	cmd.Env = env
	cmd.Dir = cwd
	// CRITICAL: do NOT call services.HideWindow here. We WANT the terminal
	// window visible - that's the whole point.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening terminal: %w", err)
	}
	// Detach. We're not waiting on the user's terminal session.
	go func() { _ = cmd.Wait() }()
	return nil
}

// --- Utility ---

func (a *App) GetVersion() string {
	return "1.0.0"
}

func (a *App) EmitEvent(event string, data interface{}) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data)
	}
}

func (a *App) errorf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}

// --- HeidiSQL methods ---

func (a *App) LaunchHeidiSQL(dbType string) error {
	dir := a.findHeidiSQLDir()
	if dir == "" {
		return fmt.Errorf("heidisql: not installed — install it from the Packages page")
	}

	// Read configured ports from app config
	mysqlPort := 3306
	pgPort := 5432
	if cfg, err := a.config.GetAppConfig(); err == nil {
		if cfg.MySQLPort > 0 {
			mysqlPort = cfg.MySQLPort
		}
		if cfg.PostgreSQLPort > 0 {
			pgPort = cfg.PostgreSQLPort
		}
	}

	// Pre-create default sessions so HeidiSQL opens with ready-to-use connections
	heidisql.EnsureDefaultSessions(dir, []heidisql.DefaultSession{
		{Name: "Hangar MySQL", Host: "127.0.0.1", Port: mysqlPort, User: "root", NetType: heidisql.NetTypeMySQLTCP},
		{Name: "Hangar PostgreSQL", Host: "127.0.0.1", Port: pgPort, User: "postgres", NetType: heidisql.NetTypePostgresTCP, Library: heidisql.FindPostgresLibrary(dir)},
	})

	port := mysqlPort
	user := "root"
	if strings.EqualFold(dbType, "postgresql") {
		port = pgPort
		user = "postgres"
	}

	return heidisql.Launch(dir, heidisql.ConnectionParams{
		Host:   "127.0.0.1",
		Port:   port,
		User:   user,
		DBType: dbType,
	})
}

// OpenHeidiSQL opens HeidiSQL session manager (no direct connection)
func (a *App) OpenHeidiSQL() error {
	dir := a.findHeidiSQLDir()
	if dir == "" {
		return fmt.Errorf("heidisql: not installed — install it from the Packages page")
	}

	// Read configured ports from app config
	mysqlPort := 3306
	pgPort := 5432
	if cfg, err := a.config.GetAppConfig(); err == nil {
		if cfg.MySQLPort > 0 {
			mysqlPort = cfg.MySQLPort
		}
		if cfg.PostgreSQLPort > 0 {
			pgPort = cfg.PostgreSQLPort
		}
	}

	// Pre-create default sessions
	heidisql.EnsureDefaultSessions(dir, []heidisql.DefaultSession{
		{Name: "Hangar MySQL", Host: "127.0.0.1", Port: mysqlPort, User: "root", NetType: heidisql.NetTypeMySQLTCP},
		{Name: "Hangar PostgreSQL", Host: "127.0.0.1", Port: pgPort, User: "postgres", NetType: heidisql.NetTypePostgresTCP, Library: heidisql.FindPostgresLibrary(dir)},
	})

	return heidisql.LaunchSessionManager(dir)
}

// ListDatabases returns the list of databases for a running DBMS
func (a *App) ListDatabases(dbType string) ([]string, error) {
	if a.serviceManager == nil {
		return nil, fmt.Errorf("service manager not initialized")
	}

	switch strings.ToLower(dbType) {
	case "mysql":
		return a.listMySQLDatabases()
	case "postgresql":
		return a.listPostgreSQLDatabases()
	default:
		return nil, fmt.Errorf("unknown database type: %s", dbType)
	}
}

func (a *App) listMySQLDatabases() ([]string, error) {
	status, err := a.serviceManager.Status("mysql")
	if err != nil || status.Status != "running" {
		return nil, nil
	}

	// Find mysql client
	mysqlExe := ""
	cfg, err := a.config.GetServiceConfig("mysql")
	if err == nil && cfg.InstallPath != "" {
		candidates := []string{
			filepath.Join(cfg.InstallPath, "bin", "mysql.exe"),
		}
		// Check nested dirs
		entries, _ := os.ReadDir(cfg.InstallPath)
		for _, e := range entries {
			if e.IsDir() {
				candidates = append(candidates, filepath.Join(cfg.InstallPath, e.Name(), "bin", "mysql.exe"))
			}
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				mysqlExe = c
				break
			}
		}
	}
	if mysqlExe == "" {
		return nil, fmt.Errorf("mysql client not found")
	}

	port := 3306
	if appCfg, err := a.config.GetAppConfig(); err == nil && appCfg.MySQLPort > 0 {
		port = appCfg.MySQLPort
	}

	cmd := exec.Command(mysqlExe, "-u", "root", "-P", fmt.Sprintf("%d", port), "-h", "127.0.0.1", "-N", "-e", "SHOW DATABASES")
	services.HideWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("mysql: listing databases: %w", err)
	}

	var dbs []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		db := strings.TrimSpace(line)
		if db != "" {
			dbs = append(dbs, db)
		}
	}
	return dbs, nil
}

func (a *App) listPostgreSQLDatabases() ([]string, error) {
	status, err := a.serviceManager.Status("postgresql")
	if err != nil || status.Status != "running" {
		return nil, nil
	}

	// Find psql client
	psqlExe := ""
	cfg, err := a.config.GetServiceConfig("postgresql")
	if err == nil && cfg.InstallPath != "" {
		candidates := []string{
			filepath.Join(cfg.InstallPath, "bin", "psql.exe"),
		}
		entries, _ := os.ReadDir(cfg.InstallPath)
		for _, e := range entries {
			if e.IsDir() {
				candidates = append(candidates,
					filepath.Join(cfg.InstallPath, e.Name(), "bin", "psql.exe"),
					filepath.Join(cfg.InstallPath, e.Name(), "pgsql", "bin", "psql.exe"),
				)
			}
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				psqlExe = c
				break
			}
		}
	}
	if psqlExe == "" {
		return nil, fmt.Errorf("psql client not found")
	}

	port := 5432
	if appCfg, err := a.config.GetAppConfig(); err == nil && appCfg.PostgreSQLPort > 0 {
		port = appCfg.PostgreSQLPort
	}

	cmd := exec.Command(psqlExe, "-U", "postgres", "-p", fmt.Sprintf("%d", port), "-h", "127.0.0.1", "-t", "-A", "-c", "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname")
	services.HideWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("postgresql: listing databases: %w", err)
	}

	var dbs []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		db := strings.TrimSpace(line)
		if db != "" {
			dbs = append(dbs, db)
		}
	}
	return dbs, nil
}

func (a *App) IsHeidiSQLInstalled() bool {
	return a.findHeidiSQLDir() != ""
}

// InstalledItem is one entry on the "Installed" page - any package that's
// already been downloaded, enriched with how to launch/open it. Distinct
// from packages.PackageInfo (which models the registry) because the
// frontend cares about a different shape: launch_kind drives which
// button to render, and exe_path / web_url tell the launcher where to go.
type InstalledItem struct {
	Name        string `json:"name"`         // package id, e.g. "phpmyadmin"
	Label       string `json:"label"`        // display, e.g. "phpMyAdmin 5.2.2"
	Version     string `json:"version"`
	Category    string `json:"category"`     // "php" | "database" | "webserver" | "nodejs" | "tools" | "golang"
	InstallPath string `json:"install_path"` // dir on disk
	LaunchKind  string `json:"launch_kind"`  // "exe" | "web" | "service" | "session" | "cli" | "runtime"
	ExePath     string `json:"exe_path,omitempty"`
	WebURL      string `json:"web_url,omitempty"`
	ServiceName string `json:"service_name,omitempty"` // for kind=service
	IsActive    bool   `json:"is_active"`
}

// ListInstalledItems returns every installed package (PHP, MySQL, tools,
// etc.) grouped-able by Category in the UI. Used by the "Installed"
// page to give users a single launcher across everything they have on
// disk, instead of forcing them to remember which page each tool lives
// on. The frontend renders one Launch button per item based on
// LaunchKind.
func (a *App) ListInstalledItems() []InstalledItem {
	if a.pkgManager == nil {
		return nil
	}
	pkgs := a.pkgManager.GetPackages("") // "" = all categories
	out := make([]InstalledItem, 0, len(pkgs))
	for _, p := range pkgs {
		if p.Status != "installed" && p.Status != "active" {
			continue
		}
		item := InstalledItem{
			Name:        p.Name,
			Label:       p.Label,
			Version:     p.Version,
			Category:    string(p.Category),
			InstallPath: p.InstallPath,
			IsActive:    p.IsActive,
		}
		// Decide how this item launches. Order matters: web > exe > service
		// > runtime so the most-useful action wins.
		switch p.Name {
		case "phpmyadmin":
			item.LaunchKind = "web"
			item.WebURL = "http://phpmyadmin.test"
		case "heidisql":
			item.LaunchKind = "session"
			item.ExePath = a.findHeidiSQLDir() // dir, launcher resolves the exe
		case "dbeaver", "vscode", "pocketbase":
			item.LaunchKind = "exe"
			item.ExePath = a.findToolExe(p.Name)
		case "composer", "wp-cli":
			item.LaunchKind = "cli"
		case "mailpit":
			item.LaunchKind = "service"
			item.ServiceName = "mailpit"
			item.WebURL = "http://127.0.0.1:8025"
		case "meilisearch":
			item.LaunchKind = "service"
			item.ServiceName = "meilisearch"
			item.WebURL = "http://127.0.0.1:7700" // built-in web UI + Swagger
		case "apache", "nginx", "caddy":
			item.LaunchKind = "service"
			item.ServiceName = p.Name
		case "mysql", "postgresql", "mongodb":
			item.LaunchKind = "service"
			item.ServiceName = p.Name
		case "adminer":
			// Adminer is a single .php file; serve it via the same
			// vhost trick we use for phpMyAdmin. URL is reserved for
			// when we wire the /adminer route into the default
			// phpmyadmin project; for now treat it as a CLI/file
			// the user copies into their project.
			item.LaunchKind = "cli"
		case "nvm-windows", "fnm", "bun", "uv", "gh", "symfony-cli", "supabase", "flyctl", "cloudflared":
			// All CLI tools: surface "launch" as opening a terminal
			// with the tool on PATH would be ideal, but for now we
			// treat them as CLI so the Installed page shows a
			// "CLI only - use Terminal" hint rather than a Launch
			// button that does nothing. The exe is still resolvable
			// via findToolExe.
			item.LaunchKind = "cli"
			item.ExePath = a.findToolExe(p.Name)
		case "php", "node", "go", "golang":
			item.LaunchKind = "runtime"
		default:
			// Unknown tool: if we can find an exe, assume that's the launcher.
			if exe := a.findToolExe(p.Name); exe != "" {
				item.LaunchKind = "exe"
				item.ExePath = exe
			} else {
				item.LaunchKind = "none"
			}
		}
		out = append(out, item)
	}
	return out
}

// OpenInExplorer reveals a folder (or selects a file) in Windows Explorer.
// Used by the Installed page's "Folder" button so users can poke at the
// extracted package contents without having to type the path. No-op if
// path doesn't exist - silently returns rather than throwing a Windows
// shell error dialog.
func (a *App) OpenInExplorer(path string) error {
	if path == "" {
		return fmt.Errorf("OpenInExplorer: empty path")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("OpenInExplorer: %s does not exist", path)
	}
	// Use the shell to handle both files (selects) and dirs (opens).
	// /select, expects a file - for a directory we just pass the path.
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		cmd := exec.Command("explorer.exe", path)
		services.HideWindow(cmd)
		_ = cmd.Start()
		go func() { _ = cmd.Wait() }()
		return nil
	}
	cmd := exec.Command("explorer.exe", "/select,", path)
	services.HideWindow(cmd)
	_ = cmd.Start()
	go func() { _ = cmd.Wait() }()
	return nil
}

// --- Generic external-tool launchers ---
//
// Each tool has its own exe-name; we resolve it under installed/<tool>/ with
// a one-level-deep nested fallback (because some ZIPs extract into a subdir
// like dbeaver/<version>/dbeaver/dbeaver.exe). Launches non-blocking via
// services.HideWindow so no console flash on Windows.

// toolExeNames maps a tool's package name to the exe filename it ships.
// Add new tools here; the launcher / installed-check picks them up automatically.
var toolExeNames = map[string]string{
	"dbeaver":    "dbeaver.exe",
	"vscode":     "Code.exe",
	"pocketbase": "pocketbase.exe",
	// v1.2 additions - all single-binary CLIs that ship in the
	// package zip at predictable paths. The findToolExe helper will
	// fall through to one-level-nested if a zip extracts into a
	// subdir (gh and supabase do this).
	"nvm-windows": "nvm.exe",
	"fnm":         "fnm.exe",
	"bun":         "bun.exe", // ships at bun-windows-x64/bun.exe but nested fallback finds it
	"uv":          "uv.exe",
	"gh":          "gh.exe", // ships at gh_VERSION_windows_amd64/bin/gh.exe
	"symfony-cli": "symfony.exe",
	"supabase":    "supabase.exe",
	"flyctl":      "flyctl.exe",
	"cloudflared": "cloudflared-windows-amd64.exe", // ships as bare .exe
}

func (a *App) findToolExe(tool string) string {
	exeName, ok := toolExeNames[tool]
	if !ok {
		return ""
	}
	baseDir := filepath.Join(a.paths.InstalledPath(), tool)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		versionDir := filepath.Join(baseDir, entry.Name())
		// Direct: installed/<tool>/<version>/<exe>
		direct := filepath.Join(versionDir, exeName)
		if _, err := os.Stat(direct); err == nil {
			return direct
		}
		// One level nested: installed/<tool>/<version>/<subdir>/<exe>
		nested, _ := os.ReadDir(versionDir)
		for _, n := range nested {
			if !n.IsDir() {
				continue
			}
			candidate := filepath.Join(versionDir, n.Name(), exeName)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

// LaunchTool spawns a tool's main executable. Non-blocking. Returns a clear
// error if the tool isn't installed yet so the frontend can surface it.
// `arg` is an optional path passed to the tool (useful for VS Code: open a
// folder/file). Empty string means launch with no args.
func (a *App) LaunchTool(tool string, arg string) error {
	exe := a.findToolExe(tool)
	if exe == "" {
		return fmt.Errorf("%s: not installed - install it from the Packages page", tool)
	}
	args := []string{}
	if arg != "" {
		args = append(args, arg)
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	services.HideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: launch failed: %w", tool, err)
	}
	// Detach so the child outlives Devour exit
	go func() { _ = cmd.Wait() }()
	return nil
}

// IsToolInstalled is a single Wails-exposed check the frontend uses to
// decide whether to render a launch button for a given tool.
func (a *App) IsToolInstalled(tool string) bool {
	return a.findToolExe(tool) != ""
}

// OpenPhpMyAdmin returns a URL the frontend can open in the user's browser.
// phpMyAdmin is a PHP web app, not an exe - it needs Apache (or Nginx) +
// PHP serving its files. We auto-create a vhost the first time, then point
// the user at http://phpmyadmin.test.
//
// Requirements: phpmyadmin package installed, Apache or Nginx running,
// at least one PHP version active. Returns an error if any are missing so
// the frontend can surface a clear "install X first" message.
func (a *App) OpenPhpMyAdmin() (string, error) {
	if a.pkgManager == nil {
		return "", fmt.Errorf("package manager not initialized")
	}
	// Find the phpmyadmin install directory by scanning installed/phpmyadmin/.
	pmaBase := filepath.Join(a.paths.InstalledPath(), "phpmyadmin")
	entries, err := os.ReadDir(pmaBase)
	if err != nil || len(entries) == 0 {
		return "", fmt.Errorf("phpMyAdmin is not installed - install it from the Packages page")
	}
	// First version dir that contains an index.php is good enough.
	var pmaDir string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(pmaBase, e.Name())
		if _, err := os.Stat(filepath.Join(candidate, "index.php")); err == nil {
			pmaDir = candidate
			break
		}
		// One-level nested (zip extracted as phpMyAdmin-X.Y.Z-english/).
		nested, _ := os.ReadDir(candidate)
		for _, n := range nested {
			if n.IsDir() {
				maybe := filepath.Join(candidate, n.Name())
				if _, err := os.Stat(filepath.Join(maybe, "index.php")); err == nil {
					pmaDir = maybe
					break
				}
			}
		}
		if pmaDir != "" {
			break
		}
	}
	if pmaDir == "" {
		return "", fmt.Errorf("phpMyAdmin install dir found but no index.php inside %s", pmaBase)
	}

	if err := writePhpMyAdminConfig(pmaDir, a.dbPort("mysql")); err != nil {
		return "", fmt.Errorf("writing phpMyAdmin config: %w", err)
	}
	// Register phpmyadmin.test as a local-only project (auto-login as root
	// must never be reachable from the LAN or the tunnel), then make sure
	// the active web server is up and serving it.
	if err := a.ensureToolProject("phpmyadmin", "phpmyadmin.test", pmaDir); err != nil {
		return "", fmt.Errorf("registering phpmyadmin.test: %w", err)
	}
	if err := a.ensureWebServer(); err != nil {
		return "", err
	}
	return "http://phpmyadmin.test", nil
}

func (a *App) findHeidiSQLDir() string {
	baseDir := filepath.Join(a.paths.InstalledPath(), "heidisql")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dir := filepath.Join(baseDir, entry.Name())
			if heidisql.IsInstalled(dir) {
				return dir
			}
			// Check nested subdirectory
			nested, _ := os.ReadDir(dir)
			for _, n := range nested {
				if n.IsDir() {
					subDir := filepath.Join(dir, n.Name())
					if heidisql.IsInstalled(subDir) {
						return subDir
					}
				}
			}
		}
	}
	return ""
}
