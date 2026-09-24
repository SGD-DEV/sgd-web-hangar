//go:build !windows

package services

// KillStale is only needed on Windows, where killed parents leave their
// children running.
func KillStale(installRoot string, exeNames ...string) int { return 0 }
