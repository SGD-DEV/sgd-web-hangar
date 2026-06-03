package cli

// daemon.go implements `hangar daemon`: a headless mode that keeps the
// MCP server bound to 127.0.0.1:3742 so AI agents stay reachable even
// when the GUI window is closed. The daemon owns the bbolt flock and
// the MCP listener; if the GUI launches while the daemon is running,
// the existing single-instance lock pulls focus to whoever owns the
// window (or no-ops if there's no window).
//
// Tray UI deliberately stays minimal here: Open GUI / Stop services /
// Quit. Anything richer belongs in the GUI - the daemon's job is to
// keep MCP alive, not be a second control surface.

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"fyne.io/systray"
	"github.com/devour-app/devour/app/core"
	"github.com/devour-app/devour/app/tray"
)

// RunDaemon stands up Core in daemon mode, starts the system-tray icon,
// and blocks on Ctrl-C / tray-Quit. Returns nil on a clean shutdown.
func RunDaemon() error {
	c, err := core.Bootstrap(core.DefaultDaemonOptions(), core.StderrLogger)
	if err != nil {
		if _, ok := err.(core.ErrAlreadyRunning); ok {
			return fmt.Errorf("Hangar is already running (GUI or another daemon). " +
				"Close it first")
		}
		return fmt.Errorf("bootstrap: %w", err)
	}

	// ctx is cancelled by either a signal or the tray Quit menu item.
	// Both routes flow through this single cancel + Shutdown path so
	// the bbolt flock and TCP listener release deterministically.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Hook OS signals first so Ctrl-C in the terminal also exits
	// cleanly. NSIS uninstall sends SIGTERM via taskkill /T - we
	// honour both.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Tray runs on its own goroutine because systray.Run blocks until
	// the menu fires Quit (which calls systray.Quit). We bridge the
	// two cancellation channels: tray quit cancels ctx; ctx cancel
	// also fires systray.Quit so a signal exit closes the tray.
	go func() {
		<-ctx.Done()
		systray.Quit()
	}()

	fmt.Println("hangar daemon started")
	fmt.Println("  MCP server: http://127.0.0.1:3742/sse")
	fmt.Println("  Stop with: Ctrl-C, the tray icon's Quit, or `hangar daemon stop`")

	tray.Start(tray.Callbacks{
		OnShow: func() {
			// In daemon mode there is no GUI window to show. Spawn
			// hangar.exe with no args so the user gets the full
			// GUI. The single-instance lock in main.go will refuse
			// to start a second GUI if one is already up, so this
			// is safe.
			if err := launchGUI(); err != nil {
				fmt.Fprintf(os.Stderr, "could not launch GUI: %v\n", err)
			}
		},
		OnQuit: func() {
			cancel()
		},
	})

	// systray.Run returned -> we're shutting down.
	fmt.Println("hangar daemon stopping...")
	c.Shutdown()
	return nil
}
