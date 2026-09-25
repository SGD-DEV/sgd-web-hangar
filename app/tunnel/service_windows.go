//go:build windows

package tunnel

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/devour-app/devour/app/winsvc"
)

func serviceState(name string) (string, string) { return winsvc.State(name) }

// RestartService stops (if running) and starts the tunnel service so a new
// config.yml takes effect.
func (m *Manager) RestartService() error { return winsvc.Restart(m.ServiceName()) }

// StopService stops the tunnel (all published sites go offline).
func (m *Manager) StopService() error { return winsvc.Stop(m.ServiceName()) }

// InstallService registers `cloudflared tunnel run` as an auto-start
// Windows service via NSSM. It needs administrator rights, so it runs a
// small PowerShell script elevated (one UAC prompt). The script also grants
// the current user start/stop/query rights on the service, so Hangar can
// restart the tunnel later without elevation.
func (m *Manager) InstallService() error {
	nssm := winsvc.FindNSSM()
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

	sid, err := winsvc.CurrentUserSID()
	if err != nil {
		return err
	}
	name := m.ServiceName()
	q := winsvc.Q
	params := fmt.Sprintf(`tunnel --no-autoupdate --config "%s" run`, cfgPath)
	logFile := filepath.Join(dir, "service.log")
	lines := []string{
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
	}
	lines = append(lines, winsvc.GrantControlScript("$name", sid)...)
	lines = append(lines, "& $nssm start $name | Out-Null")
	return winsvc.RunElevatedScript(strings.Join(lines, "\r\n"))
}

// UninstallService removes the tunnel service (elevated).
func (m *Manager) UninstallService() error {
	nssm := winsvc.FindNSSM()
	if nssm == "" {
		return fmt.Errorf("nssm.exe not found")
	}
	q := winsvc.Q
	script := strings.Join([]string{
		"$nssm = " + q(nssm),
		"& $nssm stop " + q(m.ServiceName()),
		"& $nssm remove " + q(m.ServiceName()) + " confirm",
	}, "\r\n")
	return winsvc.RunElevatedScript(script)
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
