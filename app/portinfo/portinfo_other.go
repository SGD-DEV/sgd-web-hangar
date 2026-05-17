//go:build !windows

package portinfo

import "fmt"

// OwnerOfPort is a no-op outside Windows (we'd need /proc + lsof on Linux,
// lsof on macOS - implement when those platforms ship).
func OwnerOfPort(port int) (*Owner, error) { return nil, nil }

// Kill on non-Windows would use os.FindProcess + Signal. Stubbed out until
// we actually ship Linux/macOS builds.
func Kill(pid int) error {
	return fmt.Errorf("portinfo: Kill not implemented on this platform")
}
