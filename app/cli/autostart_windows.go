//go:build windows

package cli

// autostart_windows.go - manage the per-user "Start at login" hook.
//
// Implementation choice: we write a value under
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run which is the
// standard per-user autostart entry point on Windows. Pros:
//
//   - No UAC prompt (per-user, no admin token needed).
//   - User can inspect/remove from Task Manager > Startup any time.
//   - Survives Hangar uninstall + reinstall as long as the user keeps
//     it (the installer can re-add it from the Finish page checkbox).
//
// We deliberately do NOT use a Windows Scheduled Task or a Service:
// services would require admin, and scheduled tasks confuse users
// who'd expect "Startup apps" in Settings to be the source of truth.

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows/registry"
)

const (
	runKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartVal = "Hangar"
)

// autostartCommand wires `hangar daemon autostart enable|disable|status`.
// Lives under `daemon` (not at the top level) because that's where the
// behaviour belongs - it's the daemon we want auto-started, not the
// GUI.
func newAutostartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autostart [enable|disable|status]",
		Short: "Manage Hangar's auto-start at Windows login",
		Long: "Add or remove a per-user autostart entry that launches `hangar daemon` at\n" +
			"login so the MCP server stays reachable for AI agents without you having to\n" +
			"open the Hangar GUI after every reboot.\n\n" +
			"Writes/removes a value under HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run.\n" +
			"Visible in Task Manager > Startup. No UAC prompt required.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := "status"
			if len(args) == 1 {
				action = args[0]
			}
			switch action {
			case "enable", "on":
				return autostartEnable()
			case "disable", "off":
				return autostartDisable()
			case "status", "":
				return autostartStatus()
			default:
				return fmt.Errorf("unknown action %q (valid: enable, disable, status)", action)
			}
		},
	}
	return cmd
}

func autostartEnable() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating self: %w", err)
	}

	// Quoted full path + the background flag: the GUI process starts
	// hidden in the tray and brings back the services that were running.
	// Quoted because the path may contain spaces.
	value := fmt.Sprintf(`"%s" %s`, exe, BackgroundFlag)

	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("opening Run key: %w", err)
	}
	defer key.Close()

	if err := key.SetStringValue(autostartVal, value); err != nil {
		return fmt.Errorf("writing Run value: %w", err)
	}
	fmt.Printf("Autostart ENABLED.\n  HKCU\\%s\\%s = %s\n", runKey, autostartVal, value)
	fmt.Println("  Hangar will start in the tray at the next Windows login.")
	return nil
}

func autostartDisable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			fmt.Println("Autostart already disabled (Run key does not exist).")
			return nil
		}
		return fmt.Errorf("opening Run key: %w", err)
	}
	defer key.Close()

	if err := key.DeleteValue(autostartVal); err != nil {
		if err == registry.ErrNotExist {
			fmt.Println("Autostart already disabled (no Hangar entry present).")
			return nil
		}
		return fmt.Errorf("deleting Run value: %w", err)
	}
	fmt.Printf("Autostart DISABLED. Removed HKCU\\%s\\%s\n", runKey, autostartVal)
	return nil
}

func autostartStatus() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			fmt.Println("Autostart: DISABLED (Run key does not exist)")
			return nil
		}
		return fmt.Errorf("opening Run key: %w", err)
	}
	defer key.Close()

	val, _, err := key.GetStringValue(autostartVal)
	if err != nil {
		if err == registry.ErrNotExist {
			fmt.Println("Autostart: DISABLED")
			return nil
		}
		return fmt.Errorf("reading Run value: %w", err)
	}
	fmt.Println("Autostart: ENABLED")
	fmt.Printf("  HKCU\\%s\\%s = %s\n", runKey, autostartVal, val)
	return nil
}
