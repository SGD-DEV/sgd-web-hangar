//go:build windows

package cli

// launch_windows.go - spawning the GUI from the daemon's tray menu.
// Kept platform-specific because spawning a detached child process
// without a console flash needs Windows-specific flags.

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/devour-app/devour/app/services"
)

// launchGUI starts hangar.exe with no arguments so it runs in GUI
// mode. Detached from the daemon - if the user closes the GUI window
// later, the daemon keeps running. If the GUI is already open the
// single-instance lock in main.go pulls focus instead of spawning.
func launchGUI() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating self: %w", err)
	}
	cmd := exec.Command(exe)
	services.HideWindow(cmd) // no console flash on Windows
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting GUI: %w", err)
	}
	// Detach so the GUI outlives the daemon if the daemon exits
	// later. Otherwise we accidentally orphan it.
	go func() { _ = cmd.Wait() }()
	return nil
}
