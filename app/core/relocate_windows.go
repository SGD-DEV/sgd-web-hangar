//go:build windows

package core

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// relocateUserPath rewrites entries of the user PATH that point into the old
// data folder (Hangar puts the active PHP, MySQL client etc. there).
func relocateUserPath(oldBase, newBase string, log Logger) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer key.Close()
	val, valType, err := key.GetStringValue("Path")
	if err != nil {
		return
	}
	parts := strings.Split(val, ";")
	changed := false
	for i, p := range parts {
		if len(p) >= len(oldBase) && strings.EqualFold(p[:len(oldBase)], oldBase) {
			parts[i] = newBase + p[len(oldBase):]
			changed = true
		}
	}
	if !changed {
		return
	}
	newVal := strings.Join(parts, ";")
	if valType == registry.EXPAND_SZ {
		err = key.SetExpandStringValue("Path", newVal)
	} else {
		err = key.SetStringValue("Path", newVal)
	}
	if err != nil {
		log(LevelWarn, "relocate: updating user PATH: %v", err)
		return
	}
	broadcastEnvChange()
	log(LevelInfo, "relocate: user PATH updated")
}

// broadcastEnvChange tells Explorer (and so new terminals) that the
// environment changed.
func broadcastEnvChange() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")
	env, _ := syscall.UTF16PtrFromString("Environment")
	const hwndBroadcast = 0xffff
	const wmSettingChange = 0x001A
	const smtoAbortIfHung = 0x0002
	var result uintptr
	proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 3000, uintptr(unsafe.Pointer(&result)))
}
