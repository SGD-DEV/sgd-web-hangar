//go:build windows

package portinfo

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// OwnerOfPort finds the PID and process name listening on `port` (TCP, IPv4
// or IPv6, any interface). Returns nil, nil if nothing's listening - this
// is the "no error, no owner" signal so callers can distinguish "port is
// free" from "we couldn't tell".
//
// Implementation uses `netstat -ano` to map port -> PID, then
// `tasklist /FI` to map PID -> process name. Both are built-in to Windows
// and don't need elevation.
func OwnerOfPort(port int) (*Owner, error) {
	pid, err := findListeningPID(port)
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, nil
	}
	name, _ := processName(pid) // best-effort; an empty name is fine
	return &Owner{PID: pid, Name: name, Port: port}, nil
}

// Kill terminates the process identified by pid. /F = force kill, no
// graceful shutdown. The user is in the loop here (they clicked the button)
// so we don't bother with a friendlier signal first.
func Kill(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("portinfo: invalid pid %d", pid)
	}
	cmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill PID %d: %s: %w", pid, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// findListeningPID scans `netstat -ano` output for a TCP LISTENING row whose
// local address ends in :<port>, and returns the PID column. Returns 0 if
// nothing matches.
//
// netstat -ano format on Windows:
//
//	  Proto  Local Address          Foreign Address        State           PID
//	  TCP    0.0.0.0:80             0.0.0.0:0              LISTENING       4
//	  TCP    [::]:80                [::]:0                 LISTENING       4
//
// We accept either IPv4 or IPv6 listeners and strip the column padding.
func findListeningPID(port int) (int, error) {
	cmd := exec.Command("netstat", "-ano", "-p", "TCP")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("netstat: %w", err)
	}
	suffix := ":" + strconv.Itoa(port)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		// Local address column. Match a literal ":<port>" at the very end -
		// "127.0.0.1:8080" must not match port 80 just because it contains
		// the digits.
		local := fields[1]
		if !strings.HasSuffix(local, suffix) {
			continue
		}
		// State column. Ignore TIME_WAIT, ESTABLISHED, etc. - we only care
		// about who's CURRENTLY accepting new connections.
		if !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err == nil && pid > 0 {
			return pid, nil
		}
	}
	return 0, nil
}

// processName looks up the image name for pid via `tasklist`. Returns ""
// (no error) if the PID has gone away or tasklist couldn't find it.
func processName(pid int) (string, error) {
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	// CSV output: "Image","PID","Session","Session#","MemUsage"
	// We want column 0. Strip the surrounding quotes.
	line := strings.TrimSpace(string(out))
	if line == "" || strings.HasPrefix(line, "INFO:") {
		return "", nil
	}
	first := strings.SplitN(line, ",", 2)[0]
	first = strings.Trim(first, "\"")
	return first, nil
}
