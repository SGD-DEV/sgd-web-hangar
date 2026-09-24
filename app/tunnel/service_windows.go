//go:build windows

package tunnel

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/devour-app/devour/app/services"
	"golang.org/x/sys/windows"
)

// openService opens a service with only the rights needed for the action.
// Hangar normally runs unelevated; InstallService grants the current user
// query/start/stop rights on the tunnel service so this works without UAC.
func openService(name string, access uint32) (windows.Handle, windows.Handle, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, 0, err
	}
	namePtr, _ := windows.UTF16PtrFromString(name)
	svc, err := windows.OpenService(scm, namePtr, access)
	if err != nil {
		windows.CloseServiceHandle(scm)
		return 0, 0, err
	}
	return scm, svc, nil
}

func serviceState(name string) (string, string) {
	scm, svc, err := openService(name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if err == windows.ERROR_SERVICE_DOES_NOT_EXIST {
			return "not-installed", ""
		}
		return "unknown", err.Error()
	}
	defer windows.CloseServiceHandle(scm)
	defer windows.CloseServiceHandle(svc)
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(svc, &st); err != nil {
		return "unknown", err.Error()
	}
	switch st.CurrentState {
	case windows.SERVICE_RUNNING:
		return "running", ""
	case windows.SERVICE_STOPPED:
		return "stopped", ""
	case windows.SERVICE_START_PENDING:
		return "starting", ""
	case windows.SERVICE_STOP_PENDING:
		return "stopping", ""
	}
	return "unknown", fmt.Sprintf("state %d", st.CurrentState)
}

// RestartService stops (if running) and starts the tunnel service so a new
// config.yml takes effect.
func (m *Manager) RestartService() error {
	name := m.ServiceName()
	scm, svc, err := openService(name, windows.SERVICE_QUERY_STATUS|windows.SERVICE_START|windows.SERVICE_STOP)
	if err != nil {
		if err == windows.ERROR_SERVICE_DOES_NOT_EXIST {
			return fmt.Errorf("service %q is not installed yet", name)
		}
		if err == windows.ERROR_ACCESS_DENIED {
			return fmt.Errorf("no permission to control service %q - reinstall it from the Tunnel page", name)
		}
		return err
	}
	defer windows.CloseServiceHandle(scm)
	defer windows.CloseServiceHandle(svc)

	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(svc, &st); err != nil {
		return err
	}
	if st.CurrentState != windows.SERVICE_STOPPED {
		if err := windows.ControlService(svc, windows.SERVICE_CONTROL_STOP, &st); err != nil && err != windows.ERROR_SERVICE_NOT_ACTIVE {
			return fmt.Errorf("stopping %s: %w", name, err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for st.CurrentState != windows.SERVICE_STOPPED && time.Now().Before(deadline) {
			time.Sleep(300 * time.Millisecond)
			if err := windows.QueryServiceStatus(svc, &st); err != nil {
				return err
			}
		}
	}
	if err := windows.StartService(svc, 0, nil); err != nil {
		return fmt.Errorf("starting %s: %w", name, err)
	}
	return nil
}

// StopService stops the tunnel (all published sites go offline).
func (m *Manager) StopService() error {
	scm, svc, err := openService(m.ServiceName(), windows.SERVICE_STOP|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(scm)
	defer windows.CloseServiceHandle(svc)
	var st windows.SERVICE_STATUS
	if err := windows.ControlService(svc, windows.SERVICE_CONTROL_STOP, &st); err != nil && err != windows.ERROR_SERVICE_NOT_ACTIVE {
		return err
	}
	return nil
}

// InstallService registers `cloudflared tunnel run` as an auto-start
// Windows service via NSSM. It needs administrator rights, so it runs a
// small PowerShell script elevated (one UAC prompt). The script also grants
// the current user start/stop/query rights on the service, so Hangar can
// restart the tunnel later without elevation.
func (m *Manager) InstallService() error {
	nssm := findNSSM()
	if nssm == "" {
		return fmt.Errorf("nssm.exe not found - install NSSM (winget install NSSM.NSSM) and add it to PATH")
	}
	src := m.Cloudflared()
	if src == "" {
		return fmt.Errorf("cloudflared is not installed - install it on the Packages page")
	}
	cfgPath := m.ConfigPath()
	if _, err := os.Stat(cfgPath); err != nil {
		return fmt.Errorf("create a tunnel first - %s does not exist", cfgPath)
	}
	dir := m.Dir()
	// The service runs a private copy so updating the Hangar package never
	// yanks the binary out from under a running tunnel.
	stable := filepath.Join(dir, "cloudflared.exe")
	if !strings.EqualFold(src, stable) {
		if err := copyFile(src, stable); err != nil {
			return fmt.Errorf("copying cloudflared: %w", err)
		}
	}

	sid, err := currentUserSID()
	if err != nil {
		return err
	}
	name := m.ServiceName()
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	params := fmt.Sprintf(`tunnel --no-autoupdate --config "%s" run`, cfgPath)
	logFile := filepath.Join(dir, "service.log")
	script := strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"$nssm = " + q(nssm),
		"$name = " + q(name),
		"if (Get-Service -Name $name -ErrorAction SilentlyContinue) { & $nssm stop $name | Out-Null; & $nssm remove $name confirm | Out-Null }",
		"& $nssm install $name " + q(stable),
		"& $nssm set $name AppParameters " + q(params),
		"& $nssm set $name AppDirectory " + q(dir),
		"& $nssm set $name DisplayName 'Hosting - Cloudflare Tunnel'",
		"& $nssm set $name Start SERVICE_AUTO_START",
		"& $nssm set $name AppStdout " + q(logFile),
		"& $nssm set $name AppStderr " + q(logFile),
		"& $nssm set $name AppRotateFiles 1",
		"& $nssm set $name AppRotateBytes 10485760",
		// RP=start WP=stop LC=query status RC=read: enough for Hangar.
		"$sd = (sc.exe sdshow $name | Where-Object { $_ }) -join ''",
		"$ace = '(A;;RPWPLCRC;;;" + sid + ")'",
		"if ($sd -notlike \"*$ace*\") { $i = $sd.IndexOf('D:') + 2; sc.exe sdset $name ($sd.Substring(0, $i) + $ace + $sd.Substring($i)) | Out-Null }",
		"& $nssm start $name | Out-Null",
	}, "\r\n")
	return runElevatedScript(script)
}

// UninstallService removes the tunnel service (elevated).
func (m *Manager) UninstallService() error {
	nssm := findNSSM()
	if nssm == "" {
		return fmt.Errorf("nssm.exe not found")
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := strings.Join([]string{
		"$nssm = " + q(nssm),
		"& $nssm stop " + q(m.ServiceName()),
		"& $nssm remove " + q(m.ServiceName()) + " confirm",
	}, "\r\n")
	return runElevatedScript(script)
}

// runElevatedScript runs a PowerShell script with administrator rights (one
// UAC prompt). The script reports its own failure into a log file next to
// it, because the elevated process can't hand stdout back to us.
func runElevatedScript(script string) error {
	f, err := os.CreateTemp("", "hangar-elevated-*.ps1")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	logPath := f.Name() + ".log"
	defer os.Remove(logPath)
	wrapped := "try {\r\n" + script + "\r\n} catch {\r\n  $_ | Out-String | Set-Content -LiteralPath '" + logPath + "'\r\n  exit 1\r\n}\r\nexit 0\r\n"
	// UTF-8 BOM so Windows PowerShell 5.1 reads non-ASCII paths correctly.
	if _, err := f.WriteString("\ufeff" + wrapped); err != nil {
		f.Close()
		return err
	}
	f.Close()

	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`$p = Start-Process powershell -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList '-NoProfile -ExecutionPolicy Bypass -File "%s"'; exit $p.ExitCode`, f.Name()))
	services.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if logData, readErr := os.ReadFile(logPath); readErr == nil {
			msg = strings.TrimSpace(string(logData))
		}
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "canceled by the user") || strings.Contains(lower, "abgebrochen") {
			return fmt.Errorf("the administrator prompt was declined")
		}
		return fmt.Errorf("elevated step failed: %s", firstNonEmpty(lastLines(msg, 6), err.Error()))
	}
	return nil
}

func findNSSM() string {
	if p, err := exec.LookPath("nssm"); err == nil {
		return p
	}
	for _, p := range []string{`C:\Hosting\bin\nssm.exe`, `C:\Program Files\nssm\nssm.exe`} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func currentUserSID() (string, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return tu.User.Sid.String(), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
