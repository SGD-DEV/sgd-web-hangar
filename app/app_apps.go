package app

// app_apps.go: Node / Python / ... projects that Hangar runs as Windows
// services through NSSM. The service runs as LocalService (not SYSTEM) with
// write access to its own project folder only; the web server proxies the
// project's domain to the app's port.
//
// Installing and removing need one UAC prompt. The install grants the
// current user start/stop rights and write access to the service's NSSM
// settings, so changing the command, port or environment later doesn't.

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/devour-app/devour/app/projects"
	"github.com/devour-app/devour/app/services"
	"github.com/devour-app/devour/app/winsvc"
)

const appServiceAccount = `NT AUTHORITY\LocalService`

func appServiceName(project string) string { return "Hangar-App-" + project }

func (a *App) appLogPath(project string) string {
	return filepath.Join(a.paths.DataPath(), "logs", "apps", project+".log")
}

// AppStatus is what the App dialog shows.
type AppStatus struct {
	Service string `json:"service"`
	State   string `json:"state"` // running, stopped, starting, stopping, not-installed, unknown
	Detail  string `json:"detail"`
	LogPath string `json:"log_path"`
	// Listening reports whether something answers on the app's port.
	Listening bool `json:"listening"`
}

func (a *App) GetAppStatus(name string) (AppStatus, error) {
	p, err := a.projectManager.Get(name)
	if err != nil {
		return AppStatus{}, err
	}
	st := AppStatus{Service: appServiceName(name), LogPath: a.appLogPath(name)}
	st.State, st.Detail = winsvc.State(st.Service)
	if p.App != nil {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p.App.Port), 500*time.Millisecond); err == nil {
			c.Close()
			st.Listening = true
		}
	}
	return st, nil
}

func (a *App) appInstalled(name string) bool {
	state, _ := winsvc.State(appServiceName(name))
	return state != "not-installed" && state != "unknown"
}

// SuggestAppPort returns a free port from 3100 up that no other app
// project uses.
func (a *App) SuggestAppPort() int {
	used := map[int]bool{}
	for _, p := range a.projectManager.List() {
		if p.App != nil {
			used[p.App.Port] = true
		}
	}
	for port := 3100; port < 4000; port++ {
		if used[port] {
			continue
		}
		if l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			l.Close()
			return port
		}
	}
	return 3100
}

// SaveProjectApp stores the app settings, turns the project into a proxy to
// the app's port and, when the service exists, applies and restarts it.
func (a *App) SaveProjectApp(name, folder string, cfg projects.AppService) (projects.Project, error) {
	p, err := a.projectManager.Get(name)
	if err != nil {
		return p, err
	}
	if err := projects.ValidateApp(&cfg); err != nil {
		return p, err
	}
	for _, other := range a.projectManager.List() {
		if other.Name != name && other.App != nil && other.App.Port == cfg.Port {
			return p, fmt.Errorf("Port %d wird schon von Projekt %s verwendet", cfg.Port, other.Name)
		}
	}
	folder = strings.TrimSpace(folder)
	if folder == "" {
		folder = p.Path
	}
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return p, fmt.Errorf("der Ordner %q existiert nicht", folder)
	}
	p.Path = folder
	p.DocumentRoot = folder
	p.Framework = string(projects.FrameworkProxy)
	p.ProxyTarget = fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	p.App = &cfg
	if err := a.projectManager.Update(name, p); err != nil {
		return p, err
	}
	if err := a.projectManager.EnsureProjectsReady(); err != nil {
		return p, fmt.Errorf("gespeichert, aber der Webserver konnte nicht aktualisiert werden: %w", err)
	}
	if a.appInstalled(name) {
		if err := a.applyAppService(p, true); err != nil {
			return p, fmt.Errorf("gespeichert, aber der Dienst konnte nicht aktualisiert werden: %w", err)
		}
	}
	return p, nil
}

// applyAppService writes command, folder and environment into the
// service's settings and (re)starts it when restart is set.
// appCmdParams builds cmd.exe's arguments for a start command: /d skips
// AutoRun, /s /c "..." runs the line as typed.
func appCmdParams(command string) string {
	return `/d /s /c "` + command + `"`
}

func (a *App) applyAppService(p projects.Project, restart bool) error {
	if p.App == nil {
		return fmt.Errorf("Projekt %s hat keine App-Einstellungen", p.Name)
	}
	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	err := winsvc.WriteNSSMParams(appServiceName(p.Name), winsvc.NSSMParams{
		Application: cmdExe,
		Parameters:  appCmdParams(p.App.Command),
		Directory:   p.Path,
		Environment: projects.AppEnvironment(p),
	})
	if err != nil {
		return err
	}
	if restart {
		return winsvc.Restart(appServiceName(p.Name))
	}
	return nil
}

// InstallAppService registers the app as an auto-start service (one UAC
// prompt) and starts it.
func (a *App) InstallAppService(name string) error {
	p, err := a.projectManager.Get(name)
	if err != nil {
		return err
	}
	if p.App == nil {
		return fmt.Errorf("speichere zuerst die App-Einstellungen")
	}
	nssm := winsvc.FindNSSM()
	if nssm == "" {
		return fmt.Errorf("nssm.exe nicht gefunden")
	}
	sid, err := winsvc.CurrentUserSID()
	if err != nil {
		return err
	}
	logPath := a.appLogPath(name)
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return err
	}
	q := winsvc.Q
	lines := []string{
		"$ErrorActionPreference = 'Stop'",
		"$nssm = " + q(nssm),
		"$name = " + q(appServiceName(name)),
		"if (Get-Service -Name $name -ErrorAction SilentlyContinue) { & $nssm stop $name | Out-Null; & $nssm remove $name confirm | Out-Null }",
		"& $nssm install $name " + q(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")) + " | Out-Null",
		"& $nssm set $name DisplayName " + q("Hosting - App "+name) + " | Out-Null",
		"& $nssm set $name Description " + q("Hangar app project "+name+" ("+p.Path+")") + " | Out-Null",
		"& $nssm set $name Start SERVICE_AUTO_START | Out-Null",
		"& $nssm set $name ObjectName " + q(appServiceAccount) + " '\"\"' | Out-Null",
		"& $nssm set $name AppStdout " + q(logPath) + " | Out-Null",
		"& $nssm set $name AppStderr " + q(logPath) + " | Out-Null",
		// Rotate at service start only. Online rotation (AppRotateOnline) pipes
		// the output through an NSSM thread that can keep the service stuck in
		// "stopping" forever; WriteNSSMParams turns it off for older installs.
		"& $nssm set $name AppRotateFiles 1 | Out-Null",
		"& $nssm set $name AppRotateBytes 10485760 | Out-Null",
		// Restart after a crash, but give up flapping quickly enough to see it.
		"& $nssm set $name AppThrottle 5000 | Out-Null",
		"& $nssm set $name AppExit Default Restart | Out-Null",
		"& $nssm set $name AppRestartDelay 3000 | Out-Null",
		// The app may write in its own folder (uploads, SQLite, caches) and
		// its log, nowhere else.
		"icacls " + q(p.Path) + " /grant '*S-1-5-19:(OI)(CI)M' /C /Q | Out-Null",
		"icacls " + q(filepath.Dir(logPath)) + " /grant '*S-1-5-19:(OI)(CI)M' /C /Q | Out-Null",
	}
	lines = append(lines, winsvc.GrantControlScript("$name", sid)...)
	lines = append(lines, winsvc.GrantParamsScript("$name", sid)...)
	if err := winsvc.RunElevatedScript(strings.Join(lines, "\r\n")); err != nil {
		return err
	}
	return a.applyAppService(p, true)
}

// UninstallAppService stops and removes the service (one UAC prompt). The
// project and its files stay.
func (a *App) UninstallAppService(name string) error {
	nssm := winsvc.FindNSSM()
	if nssm == "" {
		return fmt.Errorf("nssm.exe nicht gefunden")
	}
	q := winsvc.Q
	return winsvc.RunElevatedScript(strings.Join([]string{
		"$nssm = " + q(nssm),
		"& $nssm stop " + q(appServiceName(name)) + " | Out-Null",
		"& $nssm remove " + q(appServiceName(name)) + " confirm | Out-Null",
	}, "\r\n"))
}

func (a *App) StartApp(name string) error   { return winsvc.Start(appServiceName(name)) }
func (a *App) StopApp(name string) error    { return winsvc.Stop(appServiceName(name)) }
func (a *App) RestartApp(name string) error { return winsvc.Restart(appServiceName(name)) }

// GetAppLog returns the last lines of the app's output.
func (a *App) GetAppLog(name string, lines int) ([]string, error) {
	return tailFile(a.appLogPath(name), lines)
}

// DeployApp updates a running app: git pull (if the folder is a
// repository), the build command (if set), then a service restart.
func (a *App) DeployApp(name string) (string, error) {
	p, err := a.projectManager.Get(name)
	if err != nil {
		return "", err
	}
	if p.App == nil {
		return "", fmt.Errorf("Projekt %s ist keine App", name)
	}
	var log []string
	if a.isRepoRoot(p.Path) {
		out, err := a.git(p.Path, 5*time.Minute, "pull", "--ff-only")
		log = append(log, "$ git pull --ff-only", out)
		if err != nil {
			return strings.Join(log, "\n"), fmt.Errorf("git pull: %w", err)
		}
	}
	if p.App.BuildCommand != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", p.App.BuildCommand)
		cmd.Dir = p.Path
		cmd.Env = append(a.buildEnv(), projects.AppEnvironment(p)...)
		services.HideWindow(cmd)
		out, err := cmd.CombinedOutput()
		log = append(log, "$ "+p.App.BuildCommand, lastNLines(string(out), 40))
		if err != nil {
			return strings.Join(log, "\n"), fmt.Errorf("Build fehlgeschlagen: %v", err)
		}
	}
	if a.appInstalled(name) {
		if err := a.applyAppService(p, true); err != nil {
			return strings.Join(log, "\n"), err
		}
		log = append(log, "Service restarted.")
	}
	return strings.TrimSpace(strings.Join(log, "\n")), nil
}

func lastNLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	if len(lines) > n {
		lines = append([]string{fmt.Sprintf("... (%d lines)", len(lines)-n)}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n")
}
