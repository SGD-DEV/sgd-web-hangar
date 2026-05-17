//go:build windows

package syspath

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/devour-app/devour/app/config"
	"golang.org/x/sys/windows/registry"
)

// runtimeDef describes how to resolve a runtime version into a PATH entry.
type runtimeDef struct {
	Name     string
	Label    string
	Subdir   string                          // base subdirectory under data/installed/
	BinPath  func(versionDir string) string  // turns versionDir into the dir we add to PATH
}

// runtimeDefs is the registry of runtimes we know how to activate. The BinPath
// function lets each runtime use its own ZIP layout (e.g. PHP puts php.exe at
// the root, MySQL puts mysql.exe in bin/).
func runtimeDefs() []runtimeDef {
	return []runtimeDef{
		{Name: "php", Label: "PHP", Subdir: "php", BinPath: func(d string) string { return d }},
		{Name: "node", Label: "Node.js", Subdir: "node", BinPath: func(d string) string {
			// Node ZIP often extracts to a nested "node-vX.Y.Z-win-x64" subdir
			if exe := findFileBelow(d, "node.exe", 2); exe != "" {
				return filepath.Dir(exe)
			}
			return d
		}},
		{Name: "go", Label: "Go", Subdir: "go", BinPath: func(d string) string {
			// Official go1.X.Y.windows-amd64.zip extracts as a nested "go/"
			// subdir, so the actual layout is installed/go/<version>/go/bin/.
			// Fall back to bin/ directly if a future packaging is flat.
			if exe := findFileBelow(d, "go.exe", 2); exe != "" {
				return filepath.Dir(exe)
			}
			return filepath.Join(d, "bin")
		}},
		{Name: "composer", Label: "Composer", Subdir: "composer", BinPath: func(d string) string { return d }},
		{Name: "mysql", Label: "MySQL", Subdir: "mysql", BinPath: func(d string) string {
			if exe := findFileBelow(d, "mysql.exe", 2); exe != "" {
				return filepath.Dir(exe)
			}
			return filepath.Join(d, "bin")
		}},
		{Name: "postgresql", Label: "PostgreSQL", Subdir: "postgresql", BinPath: func(d string) string {
			if exe := findFileBelow(d, "psql.exe", 3); exe != "" {
				return filepath.Dir(exe)
			}
			return filepath.Join(d, "bin")
		}},
	}
}

// findFileBelow searches up to maxDepth levels under root for a file named
// target, returning the full path if found. Used to handle ZIPs that extract
// to nested subdirectories.
func findFileBelow(root, target string, maxDepth int) string {
	type frame struct {
		dir   string
		depth int
	}
	stack := []frame{{root, 0}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		entries, err := os.ReadDir(f.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				if f.depth < maxDepth {
					stack = append(stack, frame{filepath.Join(f.dir, e.Name()), f.depth + 1})
				}
				continue
			}
			if strings.EqualFold(e.Name(), target) {
				return filepath.Join(f.dir, e.Name())
			}
		}
	}
	return ""
}

type windowsManager struct {
	paths config.Paths
	// mu serializes read-modify-write of the PATH registry value. Without
	// it, two concurrent SetActiveRuntime calls (e.g. user clicks two
	// dropdowns rapidly) both read the same old PATH, both compute their
	// edits independently, and the second writer clobbers the first
	// runtime's swap.
	mu sync.Mutex
}

// New returns a Windows PATH manager backed by the user's HKCU\Environment
// registry hive. paths is used to recognise Devour-managed entries.
func New(paths config.Paths) Manager {
	return &windowsManager{paths: paths}
}

const (
	userPathKey   = `Environment`
	systemPathKey = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
)

// readPath reads the raw PATH value from the given registry hive/key. Returns
// "" if the value doesn't exist (Path can be missing from a fresh user hive).
func readPath(hive registry.Key, path string) (string, error) {
	k, err := registry.OpenKey(hive, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	// Try EXPAND_SZ first (preserves %SystemRoot% etc.), fall back to SZ.
	v, _, err := k.GetStringValue("Path")
	if err != nil {
		if err == registry.ErrNotExist {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// writeUserPath sets HKCU\Environment\Path as REG_EXPAND_SZ so values like
// %USERPROFILE% are still expanded by other processes. Writing requires no
// elevation since HKCU is per-user.
func writeUserPath(value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, userPathKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("syspath: opening HKCU\\Environment: %w", err)
	}
	defer k.Close()
	if err := k.SetExpandStringValue("Path", value); err != nil {
		return fmt.Errorf("syspath: writing Path: %w", err)
	}
	return nil
}

func (m *windowsManager) List() ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked()
}

// listLocked is List without taking the mutex — for callers that already hold
// it (e.g. GetRuntimes). Keeping these split avoids reentrant locking
// deadlocks while still letting external callers be thread-safe.
func (m *windowsManager) listLocked() ([]Entry, error) {
	userRaw, err := readPath(registry.CURRENT_USER, userPathKey)
	if err != nil {
		return nil, err
	}
	systemRaw, _ := readPath(registry.LOCAL_MACHINE, systemPathKey) // best-effort

	var out []Entry
	out = append(out, m.annotate(splitPath(userRaw), ScopeUser)...)
	out = append(out, m.annotate(splitPath(systemRaw), ScopeSystem)...)
	return out, nil
}

// splitPath splits a Windows PATH string on `;` and trims empty segments.
func splitPath(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// annotate enriches raw path strings with Devour-managed metadata.
func (m *windowsManager) annotate(paths []string, scope Scope) []Entry {
	installed := strings.ToLower(filepath.Clean(m.paths.InstalledPath()))
	out := make([]Entry, 0, len(paths))
	for _, p := range paths {
		clean := filepath.Clean(p)
		entry := Entry{Path: p, Scope: scope}
		if _, err := os.Stat(clean); err == nil {
			entry.Exists = true
		}
		lower := strings.ToLower(clean)
		if strings.HasPrefix(lower, installed) {
			entry.Managed = true
			rel := strings.TrimPrefix(lower, installed)
			rel = strings.TrimPrefix(rel, string(filepath.Separator))
			segs := strings.Split(rel, string(filepath.Separator))
			if len(segs) >= 2 {
				entry.Runtime = segs[0]
				entry.Version = segs[1]
			} else if len(segs) == 1 {
				entry.Runtime = segs[0]
			}
		}
		out = append(out, entry)
	}
	return out
}

// Add appends path to the user PATH (no-op if already present, exact match).
// scope=system is rejected because we don't escalate from inside the GUI.
func (m *windowsManager) Add(path string, scope Scope) error {
	if scope == ScopeSystem {
		return fmt.Errorf("syspath: system scope requires admin elevation — use Windows Settings instead")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := readPath(registry.CURRENT_USER, userPathKey)
	if err != nil {
		return err
	}
	parts := splitPath(raw)
	for _, p := range parts {
		if strings.EqualFold(filepath.Clean(p), filepath.Clean(path)) {
			return nil // already present
		}
	}
	parts = append([]string{path}, parts...) // prepend so it wins over system PATH
	if err := writeUserPath(strings.Join(parts, ";")); err != nil {
		return err
	}
	m.BroadcastChange()
	return nil
}

func (m *windowsManager) Remove(path string, scope Scope) error {
	if scope == ScopeSystem {
		return fmt.Errorf("syspath: system scope requires admin elevation")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := readPath(registry.CURRENT_USER, userPathKey)
	if err != nil {
		return err
	}
	parts := splitPath(raw)
	target := strings.ToLower(filepath.Clean(path))
	out := make([]string, 0, len(parts))
	removed := false
	for _, p := range parts {
		if strings.ToLower(filepath.Clean(p)) == target {
			removed = true
			continue
		}
		out = append(out, p)
	}
	if !removed {
		return nil
	}
	if err := writeUserPath(strings.Join(out, ";")); err != nil {
		return err
	}
	m.BroadcastChange()
	return nil
}

// SetActiveRuntime makes the given runtime+version the one on the user's PATH.
// Any existing user-PATH entries that point at the same runtime's data/installed/
// directory are removed first; then the new bin path is prepended. Idempotent.
func (m *windowsManager) SetActiveRuntime(runtime, version string) error {
	def := findRuntimeDef(runtime)
	if def == nil {
		return fmt.Errorf("syspath: unknown runtime %q", runtime)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	versionDir := filepath.Join(m.paths.InstalledPath(), def.Subdir, version)
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		return fmt.Errorf("syspath: %s %s is not installed at %s", runtime, version, versionDir)
	}
	binPath := def.BinPath(versionDir)
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		return fmt.Errorf("syspath: bin dir for %s %s does not exist at %s", runtime, version, binPath)
	}

	raw, err := readPath(registry.CURRENT_USER, userPathKey)
	if err != nil {
		return err
	}
	parts := splitPath(raw)

	// Drop any user-PATH entry under data/installed/<runtime>/ — replace, don't accumulate.
	runtimeRoot := strings.ToLower(filepath.Clean(filepath.Join(m.paths.InstalledPath(), def.Subdir)))
	out := make([]string, 0, len(parts)+1)
	for _, p := range parts {
		lower := strings.ToLower(filepath.Clean(p))
		if strings.HasPrefix(lower, runtimeRoot+string(filepath.Separator)) || lower == runtimeRoot {
			continue
		}
		out = append(out, p)
	}
	out = append([]string{binPath}, out...)

	if err := writeUserPath(strings.Join(out, ";")); err != nil {
		return err
	}
	m.BroadcastChange()
	return nil
}

// GetRuntimes returns one entry per known runtime, with which version (if any)
// is currently on the user PATH and the full list of installed versions.
func (m *windowsManager) GetRuntimes() ([]RuntimeInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := m.listLocked()
	if err != nil {
		return nil, err
	}
	// Map runtime name → active entry from PATH (first match wins per scope priority)
	active := map[string]Entry{}
	for _, e := range entries {
		if !e.Managed || e.Runtime == "" {
			continue
		}
		if _, seen := active[e.Runtime]; !seen {
			active[e.Runtime] = e
		}
	}

	var out []RuntimeInfo
	for _, def := range runtimeDefs() {
		ri := RuntimeInfo{Name: def.Name, Label: def.Label}
		if a, ok := active[def.Name]; ok {
			ri.ActiveVersion = a.Version
			ri.ActivePath = a.Path
		}
		ri.InstalledVersions = listInstalledVersions(filepath.Join(m.paths.InstalledPath(), def.Subdir))
		out = append(out, ri)
	}
	return out, nil
}

// listInstalledVersions returns the immediate subdirectories of dir, sorted
// descending so the newest version appears first.
func listInstalledVersions(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(versions)))
	return versions
}

func findRuntimeDef(name string) *runtimeDef {
	for _, d := range runtimeDefs() {
		if d.Name == name {
			return &d
		}
	}
	return nil
}

// BroadcastChange tells all top-level windows that the environment changed,
// so newly-spawned terminals (cmd, PowerShell, IDE shells) pick up the new
// PATH without a logoff. Without this, the registry has the new value but
// already-running shells don't see it.
func (m *windowsManager) BroadcastChange() {
	const (
		HWND_BROADCAST   = 0xffff
		WM_SETTINGCHANGE = 0x001A
		SMTO_ABORTIFHUNG = 0x0002
	)
	user32 := syscall.NewLazyDLL("user32.dll")
	send := user32.NewProc("SendMessageTimeoutW")

	envPtr, _ := syscall.UTF16PtrFromString("Environment")

	var result uintptr
	send.Call(
		uintptr(HWND_BROADCAST),
		uintptr(WM_SETTINGCHANGE),
		0,
		uintptr(unsafe.Pointer(envPtr)),
		uintptr(SMTO_ABORTIFHUNG),
		5000, // ms
		uintptr(unsafe.Pointer(&result)),
	)
}
