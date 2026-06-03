// Package core holds Hangar's shared bootstrap wiring - the bit that
// initialises the config store, service manager, package manager, MCP
// server, etc. - separated from any UI framework. This lets the GUI
// (Wails), the CLI (`hangar start mysql`), and the headless daemon
// (`hangar daemon`) all use the same managers instead of duplicating
// the startup logic three times.
//
// The Wails-specific App wraps a *Core and adds context + EventsEmit
// hooks; cmd/hangar/cli.go calls Bootstrap() directly and walks the
// managers from the command line.
//
// Logger is the single seam where each mode plugs in its own logging.
// The GUI passes a function that forwards to runtime.LogInfof; the CLI
// passes one that prints to stderr; the daemon passes one that writes
// to %LOCALAPPDATA%\Hangar\data\logs\daemon.log.
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devour-app/devour/app/bootstrap"
	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/dns"
	"github.com/devour-app/devour/app/logs"
	"github.com/devour-app/devour/app/mcp"
	"github.com/devour-app/devour/app/packages"
	"github.com/devour-app/devour/app/php"
	"github.com/devour-app/devour/app/projects"
	"github.com/devour-app/devour/app/services"
	"github.com/devour-app/devour/app/services/apache"
	"github.com/devour-app/devour/app/services/caddy"
	"github.com/devour-app/devour/app/services/mailpit"
	"github.com/devour-app/devour/app/services/meilisearch"
	"github.com/devour-app/devour/app/services/mongodb"
	"github.com/devour-app/devour/app/services/mysql"
	"github.com/devour-app/devour/app/services/nginx"
	"github.com/devour-app/devour/app/services/postgresql"
	"github.com/devour-app/devour/app/ssl"
	"github.com/devour-app/devour/app/syspath"
)

// Level mirrors the small log severity set the Wails runtime uses
// (info / warn / error). We don't expose debug here because none of
// the bootstrap call sites need it.
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Logger is the minimal logging surface Bootstrap uses. The GUI passes
// runtime.LogXxx adapters; the CLI passes a stderr writer; tests can
// pass a no-op. Keeping it func-typed instead of an interface keeps
// every caller a single line.
type Logger func(level Level, format string, args ...interface{})

// NopLogger discards everything. Useful for tests.
func NopLogger(Level, string, ...interface{}) {}

// StderrLogger prints to os.Stderr. Used by the CLI mode where the GUI
// runtime isn't available. Levels above info are prefixed so output
// piped through grep still highlights warnings/errors.
func StderrLogger(level Level, format string, args ...interface{}) {
	prefix := ""
	switch level {
	case LevelWarn:
		prefix = "WARN: "
	case LevelError:
		prefix = "ERROR: "
	}
	fmt.Fprintf(os.Stderr, prefix+format+"\n", args...)
}

// Options controls what Bootstrap turns on. Defaults are GUI-mode safe.
// CLI mode typically sets StartMCP=false (the daemon owns MCP, not the
// CLI), AutoScanProjects=false (don't slow short commands), and
// AutoCreateConfigs=false (heavyweight init only when needed).
type Options struct {
	// StartMCP launches the MCP server on 127.0.0.1:3742 inside
	// Bootstrap. Set false for short-lived CLI commands; the daemon
	// and GUI keep this true so AI agents can always reach it.
	StartMCP bool

	// AutoScanProjects walks the projects directory and refreshes the
	// manager's in-memory list. Skip for fast CLI commands.
	AutoScanProjects bool

	// AutoCreateConfigs pre-generates service config files (my.ini,
	// php.ini etc.) so config editor / web servers find them. Skip
	// when the caller just wants to inspect state.
	AutoCreateConfigs bool

	// PackageEmitter is the event sink the package manager pushes
	// download-progress events into. The GUI wires this to Wails
	// EventsEmit; CLI and daemon can pass nil to drop progress events.
	PackageEmitter func(event string, data interface{})
}

// DefaultGUIOptions returns the options the Wails app uses. Kept here
// so the GUI doesn't have to remember which knobs the CLI flipped.
func DefaultGUIOptions(emit func(event string, data interface{})) Options {
	return Options{
		StartMCP:          true,
		AutoScanProjects:  true,
		AutoCreateConfigs: true,
		PackageEmitter:    emit,
	}
}

// DefaultCLIOptions is the right shape for short-lived CLI commands -
// minimal side effects, no MCP server (the daemon owns that), no
// project scan.
func DefaultCLIOptions() Options {
	return Options{
		StartMCP:          false,
		AutoScanProjects:  false,
		AutoCreateConfigs: false,
	}
}

// DefaultDaemonOptions is for the long-running headless daemon. Same as
// GUI but with no event emitter (no UI to push events to).
func DefaultDaemonOptions() Options {
	return Options{
		StartMCP:          true,
		AutoScanProjects:  true,
		AutoCreateConfigs: true,
	}
}

// Core is the assembled set of managers. Every mode (GUI, CLI, daemon)
// works against this struct. Field order intentionally matches the
// dependency order Bootstrap initialises them in, which doubles as
// reading-order documentation.
type Core struct {
	Paths          config.Paths
	Config         *config.Store
	Logs           *logs.Store
	PathManager    syspath.Manager
	ServiceManager *services.Manager
	PHPManager     *php.Manager
	ProjectManager *projects.Manager
	SSLManager     *ssl.Manager
	DNSServer      *dns.Server
	MCPServer      *mcp.Server
	PackageManager *packages.Manager
	BootstrapMgr   *bootstrap.Manager
}

// Bootstrap stands up every manager Hangar needs. Returns a Core ready
// to use. Errors are fatal in CLI/daemon contexts; GUI mode handles
// "another instance is already running" specially via the returned
// error's message - see AlreadyRunning.
//
// Bootstrap does NOT call Start() on services or block. Callers decide
// what runs when - GUI keeps the process alive via Wails, daemon
// blocks on a signal, CLI runs one command and exits.
func Bootstrap(opts Options, log Logger) (*Core, error) {
	if log == nil {
		log = NopLogger
	}

	paths := config.NewPlatformPaths()
	if err := paths.EnsureDirectories(); err != nil {
		log(LevelError, "ensuring directories: %v", err)
		// Non-fatal: a half-created data dir often still lets the
		// rest of the wiring proceed; let later steps surface the
		// real failure if any.
	}

	pathMgr := syspath.New(paths)

	store, err := config.NewStore(paths.DBPath())
	if err != nil {
		// Most-likely cause on Windows: another Hangar process still
		// holds the bbolt flock (it minimised to tray instead of
		// exiting). Translate to AlreadyRunning so callers can show
		// the right message.
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "lock") || strings.Contains(lower, "being used") || strings.Contains(lower, "another process") {
			return nil, ErrAlreadyRunning{Underlying: err}
		}
		return nil, fmt.Errorf("opening config store: %w", err)
	}

	c := &Core{
		Paths:       paths,
		Config:      store,
		Logs:        logs.NewStore(1000),
		PathManager: pathMgr,
	}

	c.ServiceManager = services.NewManager()
	c.ServiceManager.Register("apache", apache.New(paths, store, c.Logs))
	c.ServiceManager.Register("nginx", nginx.New(paths, store, c.Logs))
	c.ServiceManager.Register("caddy", caddy.New(paths, store, c.Logs))
	c.ServiceManager.Register("mysql", mysql.New(paths, store, c.Logs))
	c.ServiceManager.Register("postgresql", postgresql.New(paths, store, c.Logs))
	c.ServiceManager.Register("mongodb", mongodb.New(paths, store, c.Logs))
	c.ServiceManager.Register("meilisearch", meilisearch.New(paths, store, c.Logs))
	c.ServiceManager.Register("mailpit", mailpit.New(paths, store, c.Logs))

	c.PHPManager = php.NewManager(paths, store)
	c.ProjectManager = projects.NewManager(paths, store, c.ServiceManager)
	c.SSLManager = ssl.NewManager(paths, store)
	c.ProjectManager.SetSSLGenerator(projects.NewSSLAdapter(c.SSLManager))

	c.DNSServer = dns.NewServer(store)
	c.MCPServer = mcp.NewServer(c.ServiceManager, c.PHPManager, c.ProjectManager, c.Logs, c.Config, paths)
	c.PackageManager = packages.NewManager(paths, store, opts.PackageEmitter)
	c.BootstrapMgr = bootstrap.NewManager(paths, c.PackageManager, opts.PackageEmitter)

	// Sync ProjectsRoot + active PHP into the config so existing flows
	// (CLI php switch, MCP list_projects) see correct defaults.
	if appCfg, err := store.GetAppConfig(); err == nil {
		changed := false
		correctRoot := paths.ProjectsPath()
		if appCfg.ProjectsRoot != correctRoot {
			appCfg.ProjectsRoot = correctRoot
			changed = true
		}
		if appCfg.ActivePHP == "" {
			if versions := c.PHPManager.ListInstalled(); len(versions) > 0 {
				appCfg.ActivePHP = versions[0].Version
				changed = true
			}
		}
		if changed {
			_ = store.SaveAppConfig(appCfg)
		}
	}

	if opts.AutoScanProjects {
		c.ProjectManager.Scan()
	}

	if opts.AutoCreateConfigs {
		c.ensureServiceConfigs(log)
	}

	if opts.StartMCP {
		if err := c.MCPServer.Start(); err != nil {
			// MCP failure is non-fatal: the rest of Hangar still
			// works, the user can fix the port collision later
			// from Settings.
			log(LevelWarn, "MCP server failed to auto-start: %v (try changing port in Settings)", err)
		} else {
			log(LevelInfo, "MCP server listening on 127.0.0.1:3742")
		}
	}

	return c, nil
}

// Shutdown stops every long-running goroutine Bootstrap kicked off, in
// reverse dependency order. Idempotent and safe to call on a partially
// initialised Core (each nil check skips that component).
func (c *Core) Shutdown() {
	if c == nil {
		return
	}
	if c.MCPServer != nil {
		c.MCPServer.Stop()
	}
	if c.DNSServer != nil {
		c.DNSServer.Stop()
	}
	if c.ServiceManager != nil {
		c.ServiceManager.StopAll()
	}
	if c.Config != nil {
		_ = c.Config.Close()
	}
}

// ensureServiceConfigs pre-generates MySQL/PostgreSQL config files so
// the config editor doesn't show "file not found" the first time it's
// opened. Skipped in CLI mode where the user runs explicit commands.
func (c *Core) ensureServiceConfigs(log Logger) {
	mysqlIniPath := filepath.Join(c.Paths.ConfPath("mysql"), "my.ini")
	if _, err := os.Stat(mysqlIniPath); os.IsNotExist(err) {
		if svc, err := c.ServiceManager.Get("mysql"); err == nil {
			type configEnsurer interface{ EnsureConfig() error }
			if gen, ok := svc.(configEnsurer); ok {
				if err := gen.EnsureConfig(); err != nil {
					log(LevelWarn, "ensuring mysql config: %v", err)
				}
			}
		}
	}
}

// ErrAlreadyRunning signals that bbolt couldn't lock the config DB,
// which on Windows almost always means another Hangar process is alive
// (typically the tray daemon). Callers should surface this to the user
// with a "check the tray" hint rather than the raw flock error.
type ErrAlreadyRunning struct {
	Underlying error
}

func (e ErrAlreadyRunning) Error() string {
	return "another Hangar process is already running (check the system tray)"
}

func (e ErrAlreadyRunning) Unwrap() error { return e.Underlying }
