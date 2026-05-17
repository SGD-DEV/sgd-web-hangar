//go:build !windows

package services

import "os/exec"

// HideWindow is a no-op on non-Windows platforms
func HideWindow(cmd *exec.Cmd) {}
