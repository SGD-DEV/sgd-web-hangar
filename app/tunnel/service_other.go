//go:build !windows

package tunnel

import "fmt"

var errUnsupported = fmt.Errorf("the tunnel service is only managed on Windows")

func serviceState(string) (string, string) { return "unknown", "not supported on this OS" }
func (m *Manager) RestartService() error   { return errUnsupported }
func (m *Manager) StopService() error      { return errUnsupported }
func (m *Manager) InstallService() error   { return errUnsupported }
func (m *Manager) UninstallService() error { return errUnsupported }
