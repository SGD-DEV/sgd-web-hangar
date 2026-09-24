//go:build windows

package services

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// KillStale terminates processes named one of exeNames whose executable
// lives under installRoot. They are leftovers of a Hangar that was killed or
// crashed: nobody manages them any more, yet they still hold ports (80, 8025,
// the PHP FastCGI ports), so freshly started services fail to bind.
// Returns the number of processes killed.
func KillStale(installRoot string, exeNames ...string) int {
	want := map[string]bool{}
	for _, n := range exeNames {
		want[strings.ToLower(n)] = true
	}
	root := strings.ToLower(filepath.Clean(installRoot)) + `\`

	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)

	killed := 0
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if !want[strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))] {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, pe.ProcessID)
		if err != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_PATH*2)
		size := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) == nil {
			path := strings.ToLower(windows.UTF16ToString(buf[:size]))
			if strings.HasPrefix(path, root) && windows.TerminateProcess(h, 1) == nil {
				killed++
			}
		}
		windows.CloseHandle(h)
	}
	return killed
}
