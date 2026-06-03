//go:build !windows

package cli

// launch_other.go - GUI spawn stub for non-Windows builds. Hangar is
// Windows-first today; this exists only so the package compiles for
// developers running `go build ./...` on macOS or Linux.

import "fmt"

func launchGUI() error {
	return fmt.Errorf("launching the GUI from the daemon is only implemented on Windows")
}
