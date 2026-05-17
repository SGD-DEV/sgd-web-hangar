//go:build windows

package postgresql

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// applyRestrictedToken arranges for cmd to be launched with a Windows token
// that has the BUILTIN\Administrators SID disabled (DENY_ONLY) and all
// privileges dropped (DISABLE_MAX_PRIVILEGE). This is the standard workaround
// for postgres.exe's "Execution of PostgreSQL by a user with administrative
// permissions is not permitted" abort: Devour itself runs elevated to edit
// hosts, but postgres refuses to start under an admin token.
//
// When the current process is NOT elevated, this is a no-op — postgres will
// be spawned normally with the inherited (already-unprivileged) token.
//
// The restricted token handle is leaked intentionally: it must outlive
// CreateProcess, and on a single-shot launch the kernel reclaims it when the
// parent (Devour) exits. This avoids ordering subtleties around when exec
// internally consumes the token.
func applyRestrictedToken(cmd *exec.Cmd) error {
	elevated, err := isElevated()
	if err != nil || !elevated {
		return nil
	}
	rt, err := createRestrictedToken()
	if err != nil {
		return fmt.Errorf("creating restricted token: %w", err)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Token = syscall.Token(rt)
	return nil
}

func isElevated() (bool, error) {
	var t windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &t); err != nil {
		return false, err
	}
	defer t.Close()
	return t.IsElevated(), nil
}

// createRestrictedToken duplicates the current process token and returns a
// version with Administrators set DENY_ONLY and all privileges removed.
func createRestrictedToken() (windows.Token, error) {
	var processToken windows.Token
	if err := windows.OpenProcessToken(
		windows.CurrentProcess(),
		windows.TOKEN_DUPLICATE|windows.TOKEN_ADJUST_DEFAULT|windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY,
		&processToken,
	); err != nil {
		return 0, fmt.Errorf("OpenProcessToken: %w", err)
	}
	defer processToken.Close()

	// BUILTIN\Administrators = S-1-5-32-544
	ntAuthority := windows.SidIdentifierAuthority{Value: [6]byte{0, 0, 0, 0, 0, 5}}
	const (
		securityBuiltinDomainRID = 0x20
		domainAliasRIDAdmins     = 0x220
	)
	var adminSid *windows.SID
	if err := windows.AllocateAndInitializeSid(
		&ntAuthority,
		2,
		securityBuiltinDomainRID,
		domainAliasRIDAdmins,
		0, 0, 0, 0, 0, 0,
		&adminSid,
	); err != nil {
		return 0, fmt.Errorf("AllocateAndInitializeSid: %w", err)
	}
	defer windows.FreeSid(adminSid)

	sidsToDisable := [1]windows.SIDAndAttributes{
		{Sid: adminSid, Attributes: 0},
	}

	// CreateRestrictedToken isn't exposed in x/sys/windows, call advapi32 directly.
	advapi32 := windows.NewLazySystemDLL("advapi32.dll")
	procCreateRestrictedToken := advapi32.NewProc("CreateRestrictedToken")
	// Use LUA_TOKEN (Limited User Account) instead of DISABLE_MAX_PRIVILEGE.
	// LUA produces a token equivalent to running as a normal non-admin user:
	// the Administrators SID is set to DENY_ONLY (which is what postgres'
	// CheckTokenMembership() looks at to decide whether to refuse to start)
	// but normal privileges and DLL-load capabilities are preserved.
	// DISABLE_MAX_PRIVILEGE strips ALL privileges and was triggering
	// STATUS_DLL_INIT_FAILED (0xc0000142) when initdb's bin/ DLLs couldn't
	// be loaded through the resulting ultra-restricted ACL evaluation.
	const luaToken = 0x4

	var restricted windows.Token
	r1, _, lastErr := procCreateRestrictedToken.Call(
		uintptr(processToken),
		uintptr(luaToken),
		uintptr(len(sidsToDisable)),
		uintptr(unsafe.Pointer(&sidsToDisable[0])),
		0, 0,
		0, 0,
		uintptr(unsafe.Pointer(&restricted)),
	)
	if r1 == 0 {
		return 0, fmt.Errorf("CreateRestrictedToken: %w", lastErr)
	}
	return restricted, nil
}
