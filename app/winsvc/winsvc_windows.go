//go:build windows

// Package winsvc manages the Windows services Hangar installs through NSSM
// (the Cloudflare Tunnel, app projects). Hangar runs unelevated: installing
// needs one UAC prompt, which also grants the current user the rights to
// start/stop the service afterwards, so day-to-day control needs none.
package winsvc

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/devour-app/devour/app/services"
	"golang.org/x/sys/windows"
)

// openService opens a service with only the rights needed for the action.
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

// State returns "running", "stopped", "starting", "stopping",
// "not-installed" or "unknown" plus a detail message.
func State(name string) (string, string) {
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

func controlErr(name string, err error) error {
	if err == windows.ERROR_SERVICE_DOES_NOT_EXIST {
		return fmt.Errorf("service %q is not installed yet", name)
	}
	if err == windows.ERROR_ACCESS_DENIED {
		return fmt.Errorf("no permission to control service %q - install it again from Hangar", name)
	}
	return err
}

// Restart stops (if running) and starts a service.
func Restart(name string) error {
	scm, svc, err := openService(name, windows.SERVICE_QUERY_STATUS|windows.SERVICE_START|windows.SERVICE_STOP)
	if err != nil {
		return controlErr(name, err)
	}
	defer windows.CloseServiceHandle(scm)
	defer windows.CloseServiceHandle(svc)

	if err := stopAndWait(name, svc); err != nil {
		return err
	}
	if err := windows.StartService(svc, 0, nil); err != nil {
		return fmt.Errorf("starting %s: %w", name, err)
	}
	return nil
}

// Start starts a stopped service; a running one is left alone.
func Start(name string) error {
	scm, svc, err := openService(name, windows.SERVICE_QUERY_STATUS|windows.SERVICE_START)
	if err != nil {
		return controlErr(name, err)
	}
	defer windows.CloseServiceHandle(scm)
	defer windows.CloseServiceHandle(svc)
	if err := windows.StartService(svc, 0, nil); err != nil && err != windows.ERROR_SERVICE_ALREADY_RUNNING {
		return fmt.Errorf("starting %s: %w", name, err)
	}
	return nil
}

// Stop stops a service and waits until it is down.
func Stop(name string) error {
	scm, svc, err := openService(name, windows.SERVICE_STOP|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return controlErr(name, err)
	}
	defer windows.CloseServiceHandle(scm)
	defer windows.CloseServiceHandle(svc)
	return stopAndWait(name, svc)
}

func stopAndWait(name string, svc windows.Handle) error {
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(svc, &st); err != nil {
		return err
	}
	if st.CurrentState == windows.SERVICE_STOPPED {
		return nil
	}
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
	return nil
}

// RunElevatedScript runs a PowerShell script with administrator rights (one
// UAC prompt). The script reports its own failure into a log file next to
// it, because the elevated process can't hand stdout back to us.
func RunElevatedScript(script string) error {
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
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("elevated step failed: %s", LastLines(msg, 6))
	}
	return nil
}

// FindNSSM locates nssm.exe.
func FindNSSM() string {
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

// CurrentUserSID is the SID of the user Hangar runs as.
func CurrentUserSID() (string, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return tu.User.Sid.String(), nil
}

// GrantControlScript returns PowerShell lines that let the user with sid
// start, stop and query the service (RP=start WP=stop LC=query RC=read).
func GrantControlScript(serviceVar, sid string) []string {
	return []string{
		"$sd = (sc.exe sdshow " + serviceVar + " | Where-Object { $_ }) -join ''",
		"$ace = '(A;;RPWPLCRC;;;" + sid + ")'",
		"if ($sd -notlike \"*$ace*\") { $i = $sd.IndexOf('D:') + 2; sc.exe sdset " + serviceVar + " ($sd.Substring(0, $i) + $ace + $sd.Substring($i)) | Out-Null }",
	}
}

// Q quotes s as a PowerShell single-quoted string.
func Q(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// LastLines returns the last n non-empty-trimmed lines of s.
func LastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
