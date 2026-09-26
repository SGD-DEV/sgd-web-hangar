package app

// app_hosting.go holds the pieces that turn Hangar from a dev tool into a
// small always-on hosting panel: remembering which services should run,
// bringing them back after a reboot, restarting them when they crash, and
// switching the active web server.

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/devour-app/devour/app/phpfcgi"
	"github.com/devour-app/devour/app/projects"
	"github.com/devour-app/devour/app/services"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const watchdogInterval = 20 * time.Second

// watchdogState tracks restart attempts per service so a service that
// cannot start (broken config, port taken) is retried with growing pauses
// instead of every 20 seconds forever.
type watchdogState struct {
	mu       sync.Mutex
	failures map[string]int
	nextTry  map[string]time.Time
	started  bool
}

var watchdog = watchdogState{failures: map[string]int{}, nextTry: map[string]time.Time{}}

func isWebServer(name string) bool { return name == "apache" || name == "nginx" }

// setDesired records whether the user wants a service running. Web servers
// are exclusive: wanting one drops the other.
func (a *App) setDesired(name string, want bool) {
	if a.config == nil {
		return
	}
	cfg, err := a.config.GetAppConfig()
	if err != nil {
		return
	}
	set := map[string]bool{}
	for _, s := range cfg.DesiredServices {
		set[s] = true
	}
	if want {
		set[name] = true
		if isWebServer(name) {
			other := "nginx"
			if name == "nginx" {
				other = "apache"
			}
			delete(set, other)
			cfg.ActiveWebServer = name
		}
	} else {
		delete(set, name)
	}
	cfg.DesiredServices = cfg.DesiredServices[:0]
	for s := range set {
		cfg.DesiredServices = append(cfg.DesiredServices, s)
	}
	sort.Strings(cfg.DesiredServices)
	_ = a.config.SaveAppConfig(cfg)

	watchdog.mu.Lock()
	delete(watchdog.failures, name)
	delete(watchdog.nextTry, name)
	watchdog.mu.Unlock()
}

// autostartServices starts every service the user left running last time
// (if AutoStartAll is on) and then launches the watchdog. Runs in the
// background so the window appears immediately.
func (a *App) autostartServices() {
	if a.config == nil || a.serviceManager == nil {
		return
	}
	cfg, err := a.config.GetAppConfig()
	if err != nil {
		return
	}
	// A Hangar that was killed or crashed leaves its web server / PHP /
	// mail children running; they hold the ports and serve stale configs.
	// (MySQL and PostgreSQL are adopted by their services instead.)
	if n := services.KillStale(a.paths.InstalledPath(), "httpd.exe", "nginx.exe", "php-cgi.exe", "mailpit.exe", "caddy.exe",
		"meilisearch-windows-amd64.exe", "meilisearch.exe"); n > 0 {
		a.logStore.AddWithLevel("hangar", fmt.Sprintf("Cleaned up %d leftover process(es) from a previous Hangar run", n), "warn")
	}
	if cfg.AutoStartAll {
		// Databases first so sites that connect on boot find them.
		order := append([]string(nil), cfg.DesiredServices...)
		sort.SliceStable(order, func(i, j int) bool {
			return !isWebServer(order[i]) && isWebServer(order[j])
		})
		for _, name := range order {
			if !a.isInstalled(name) {
				continue
			}
			if err := a.startServiceNow(name); err != nil {
				a.logStore.AddWithLevel("hangar", fmt.Sprintf("Autostart %s failed: %v", name, err), "error")
			} else {
				a.logStore.Add("hangar", "Autostart: "+name+" started")
			}
		}
	}
	a.startWatchdog()
}

// startServiceNow runs the same preparation as StartService but blocks
// until the service reports back (used by autostart and the watchdog).
func (a *App) startServiceNow(name string) error {
	if isWebServer(name) {
		a.ensurePHPIni()
		if a.projectManager != nil {
			if err := a.projectManager.EnsureProjectsReady(); err != nil {
				a.logStore.AddWithLevel("hangar", "Preparing projects: "+err.Error(), "warn")
			}
		}
	}
	return a.serviceManager.Start(name)
}

func (a *App) startWatchdog() {
	watchdog.mu.Lock()
	if watchdog.started {
		watchdog.mu.Unlock()
		return
	}
	watchdog.started = true
	watchdog.mu.Unlock()

	go func() {
		t := time.NewTicker(watchdogInterval)
		defer t.Stop()
		for range t.C {
			a.watchdogTick()
		}
	}()
}

func (a *App) watchdogTick() {
	if a.config == nil || a.serviceManager == nil {
		return
	}
	cfg, err := a.config.GetAppConfig()
	if err != nil {
		return
	}
	for _, name := range cfg.DesiredServices {
		if !a.isInstalled(name) {
			continue
		}
		st, err := a.serviceManager.Status(name)
		if err != nil || st.Status != services.StatusStopped {
			continue // running, starting, initializing or stopping
		}
		watchdog.mu.Lock()
		if time.Now().Before(watchdog.nextTry[name]) {
			watchdog.mu.Unlock()
			continue
		}
		watchdog.mu.Unlock()

		a.logStore.AddWithLevel("hangar", "Watchdog: "+name+" is not running, restarting", "warn")
		err = a.startServiceNow(name)

		watchdog.mu.Lock()
		if err != nil {
			watchdog.failures[name]++
			// 20s, 40s, 80s ... capped at 10 minutes.
			wait := watchdogInterval << uint(min(watchdog.failures[name], 5))
			if wait > 10*time.Minute {
				wait = 10 * time.Minute
			}
			watchdog.nextTry[name] = time.Now().Add(wait)
			a.logStore.AddWithLevel("hangar", fmt.Sprintf("Watchdog: %s failed to start (%v), next try in %s", name, err, wait), "error")
		} else {
			delete(watchdog.failures, name)
			delete(watchdog.nextTry, name)
			a.logStore.Add("hangar", "Watchdog: "+name+" is back up")
		}
		watchdog.mu.Unlock()
		a.emit("services:changed", nil)
	}
}

func (a *App) isInstalled(name string) bool {
	svc, err := a.serviceManager.Get(name)
	return err == nil && svc.IsInstalled()
}

func (a *App) emit(event string, data interface{}) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data)
	}
}

// --- Web server switch ---

// GetActiveWebServer returns "apache" or "nginx".
func (a *App) GetActiveWebServer() string {
	if a.config == nil {
		return "apache"
	}
	cfg, err := a.config.GetAppConfig()
	if err != nil || cfg.ActiveWebServer == "" {
		return "apache"
	}
	return cfg.ActiveWebServer
}

// SwitchWebServer stops the other web server and starts target on the same
// port, so every project URL keeps working. Rolls back to the previous
// server if target fails to start.
func (a *App) SwitchWebServer(target string) error {
	if !isWebServer(target) {
		return fmt.Errorf("unknown web server %q", target)
	}
	if a.serviceManager == nil {
		return fmt.Errorf("service manager not initialized")
	}
	other := "nginx"
	if target == "nginx" {
		other = "apache"
	}
	svc, err := a.serviceManager.Get(target)
	if err != nil {
		return err
	}
	if !svc.IsInstalled() {
		return fmt.Errorf("%s ist nicht installiert - installiere es zuerst auf der Seite Pakete", target)
	}

	otherWasRunning := false
	if st, err := a.serviceManager.Status(other); err == nil && st.Status == services.StatusRunning {
		otherWasRunning = true
		if err := a.serviceManager.Stop(other); err != nil {
			return fmt.Errorf("stopping %s: %w", other, err)
		}
		// Give Windows a moment to release the listening socket.
		time.Sleep(700 * time.Millisecond)
	}

	if st, err := a.serviceManager.Status(target); err == nil && st.Status == services.StatusRunning {
		a.setDesired(target, true)
		return nil
	}
	if err := a.startServiceNow(target); err != nil {
		if otherWasRunning {
			_ = a.startServiceNow(other)
		}
		return fmt.Errorf("starting %s: %w", target, err)
	}
	a.setDesired(target, true)
	return nil
}

// --- Projects ---

// UpdateProjectSettings applies the Edit dialog and reloads the running web
// server so the change is live immediately.
func (a *App) UpdateProjectSettings(name string, s projects.ProjectSettings) (projects.Project, error) {
	if a.projectManager == nil {
		return projects.Project{}, fmt.Errorf("project manager not initialized")
	}
	var oldAliases []string
	if old, err := a.projectManager.Get(name); err == nil {
		oldAliases = old.Aliases
	}
	p, err := a.projectManager.UpdateSettings(name, s)
	if err != nil {
		return projects.Project{}, err
	}
	if err := a.projectManager.EnsureProjectsReady(); err != nil {
		return p, fmt.Errorf("gespeichert, aber der Webserver konnte nicht vorbereitet werden: %w", err)
	}
	if strings.Join(oldAliases, ",") != strings.Join(p.Aliases, ",") {
		if err := a.autoPublishTunnel(); err != nil {
			return p, fmt.Errorf("gespeichert, aber der Tunnel konnte nicht aktualisiert werden: %w", err)
		}
	}
	return p, nil
}

// OpenURL opens a URL in the default browser.
func (a *App) OpenURL(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("refusing to open %q", url)
	}
	if a.ctx == nil {
		return fmt.Errorf("app not started")
	}
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

// --- PHP pool ---

func (a *App) GetPHPPoolStatus() []phpfcgi.VersionStatus {
	if a.phpPool == nil {
		return nil
	}
	return a.phpPool.Status()
}

// GetHangarLogs returns Hangar's own log (autostart, watchdog, PHP pool).
func (a *App) GetHangarLogs(lines int) []string {
	if a.logStore == nil {
		return nil
	}
	out := a.logStore.Get("hangar", lines)
	return append(out, a.logStore.Get("php", lines)...)
}
