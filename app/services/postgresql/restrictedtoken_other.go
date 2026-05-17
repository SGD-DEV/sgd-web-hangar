//go:build !windows

package postgresql

import "os/exec"

// applyRestrictedToken is a no-op outside Windows. The "postgres refuses to
// run as administrator" check is Windows-specific.
func applyRestrictedToken(_ *exec.Cmd) error { return nil }
