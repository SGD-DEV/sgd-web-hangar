// Package portinfo identifies the process holding a TCP port and lets the
// caller terminate it. Used by Devour to turn a useless "bind() failed"
// error into "Apache (PID 1234) is using port 80 - kill it?".
//
// Windows only for now; stubs out cleanly on other platforms via build tags.
package portinfo

// Owner describes a process holding a TCP port.
type Owner struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
	Port int    `json:"port"`
}
