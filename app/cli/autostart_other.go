//go:build !windows

package cli

// autostart_other.go - non-Windows stub so the package compiles on
// macOS/Linux dev machines. Once we add Linux desktop entries and
// macOS LaunchAgents this file will gain real implementations.

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAutostartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "autostart [enable|disable|status]",
		Short: "Manage Hangar's auto-start at login (Windows-only today)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("autostart is only implemented on Windows in this build")
		},
	}
}
