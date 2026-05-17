// Package tray runs Devour's system tray icon (the "top-right arrow" area
// on Windows) so the user can keep services running while the main window
// is hidden. Mirrors what Laragon does.
//
// Design:
//   - The tray runs in its own goroutine. fyne.io/systray on Windows uses
//     pure x/sys/windows syscalls (no CGO) so spawning it from a goroutine
//     alongside Wails works.
//   - Two menu items: Show Devour (restores the hidden window) and Quit
//     (stops services and exits the process).
//   - The tray and the Wails window communicate through callbacks the host
//     supplies via Run(), so this package doesn't import app/ and avoids
//     an import cycle.
package tray

import (
	_ "embed"
	"sync"

	"fyne.io/systray"
)

// Callbacks lets the host wire tray actions to its own functions.
//
//	OnShow  - bring the Wails window back to the foreground
//	OnQuit  - stop running services and exit the process
type Callbacks struct {
	OnShow func()
	OnQuit func()
}

// IconBytes is the icon Wails will display in the tray. We embed the same
// .ico used by the installer so the look is consistent across the install
// dialog, taskbar, and tray.
//
//go:embed icon.ico
var IconBytes []byte

var (
	startOnce sync.Once
	stopFn    func()
)

// Run starts the tray. Blocks until Stop() is called or the OS asks the
// tray to exit. Call from a goroutine.
func Run(cb Callbacks) {
	systray.Run(func() {
		systray.SetIcon(IconBytes)
		systray.SetTitle("Hangar")
		systray.SetTooltip("Hangar - Local Web Dev Environment")

		mShow := systray.AddMenuItem("Show Hangar", "Restore the main window")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit Hangar", "Stop all services and exit")

		go func() {
			for {
				select {
				case <-mShow.ClickedCh:
					if cb.OnShow != nil {
						cb.OnShow()
					}
				case <-mQuit.ClickedCh:
					if cb.OnQuit != nil {
						cb.OnQuit()
					}
					systray.Quit()
					return
				}
			}
		}()
	}, nil)
}

// Start is a convenience that runs the tray on a goroutine exactly once
// per process. Safe to call multiple times - extra calls are no-ops.
func Start(cb Callbacks) {
	startOnce.Do(func() {
		stopFn = systray.Quit
		go Run(cb)
	})
}

// Stop tells the tray to exit. Safe to call before Start() or multiple times.
func Stop() {
	if stopFn != nil {
		stopFn()
	}
}
