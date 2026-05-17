package services

import (
	"os/exec"
	"syscall"
)

// HideWindow sets the process to run without a visible console window (Windows only)
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
