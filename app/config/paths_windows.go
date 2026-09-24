//go:build windows

package config

import (
	"os"
	"path/filepath"
	"strings"
)

func NewPlatformPaths() Paths {
	// 1. Explicit override via env var
	for _, env := range []string{"HANGAR_HOME", "DEVOUR_HOME"} {
		if base := os.Getenv(env); base != "" {
			return &basePaths{base: base}
		}
	}

	// 2. Resolve from exe location
	exePath, err := os.Executable()
	if err != nil {
		// Absolute fallback - should never happen
		return &basePaths{base: `C:\Devour`}
	}

	exeDir := filepath.Dir(exePath)

	// 3. hangar-home.txt next to the exe names the data folder. Unlike an
	// environment variable it also applies to the autostart entry and to
	// processes started before the variable was set.
	if data, err := os.ReadFile(filepath.Join(exeDir, "hangar-home.txt")); err == nil {
		if base := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff")); base != "" {
			return &basePaths{base: base}
		}
	}

	// Dev mode: exe is in project root (wails dev) - wails.json is next to it.
	// Data lives next to the exe so testing artifacts stay in the repo and
	// don't leak into a real user profile.
	if _, err := os.Stat(filepath.Join(exeDir, "wails.json")); err == nil {
		return &basePaths{base: exeDir}
	}

	// Production build: exe is in build/bin/ - go up 2 levels to project root
	// e.g. D:\Projects\...\larax\build\bin\devour.exe -> D:\Projects\...\larax
	candidate := filepath.Dir(filepath.Dir(exeDir))
	if _, err := os.Stat(filepath.Join(candidate, "wails.json")); err == nil {
		return &basePaths{base: candidate}
	}

	// Production install: data goes to %LOCALAPPDATA%\Hangar, NOT next to
	// the exe. Program Files has admin-only writes and hostile ACLs for
	// child processes running with a restricted token (postgres aborts
	// 0xc0000142 STATUS_DLL_INIT_FAILED on those). LOCALAPPDATA is
	// per-user, writable, matches Windows convention.
	//
	// Migration from the old "Devour" name: if a user has data at the old
	// %LOCALAPPDATA%\Devour\ path and the new %LOCALAPPDATA%\Hangar\ doesn't
	// exist, rename the old dir into place. One-time, idempotent.
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		newBase := filepath.Join(localAppData, "Hangar")
		oldBase := filepath.Join(localAppData, "Devour")
		if _, err := os.Stat(newBase); os.IsNotExist(err) {
			if _, oldErr := os.Stat(oldBase); oldErr == nil {
				// Rename succeeds atomically on the same volume. If it
				// fails (folder open in another process, etc.) fall through
				// and use the new path - user's old data stays at oldBase
				// and they can move it manually.
				_ = os.Rename(oldBase, newBase)
			}
		}
		return &basePaths{base: newBase}
	}

	// Last-resort fallback if LOCALAPPDATA is somehow unset (shouldn't happen
	// on any sane Windows install). Use exe dir as before.
	return &basePaths{base: exeDir}
}
