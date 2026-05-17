//go:build darwin

package syspath

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/devour-app/devour/app/atomicfile"
	"github.com/devour-app/devour/app/config"
)

// macOS PATH management. Unlike Windows where PATH lives in the registry,
// macOS PATH is composed at shell startup from rc files. Devour writes
// (and rewrites) a single managed block in the user's ~/.zshrc bracketed
// by sentinel comments — that way:
//   1. We own that block: rewriting it doesn't damage anything else.
//   2. Other tools' edits to .zshrc above/below survive untouched.
//   3. The user can safely delete the block to revert.
//
// New shells pick up the change automatically the next time they read
// ~/.zshrc (i.e., open a new terminal). There is no equivalent of
// WM_SETTINGCHANGE on macOS, so already-open shells don't see it —
// the UI tells the user to open a new terminal, same as Windows.
//
// We target ~/.zshrc because zsh has been the macOS default since
// Catalina. Users on bash typically have ~/.bash_profile; we don't
// touch that — those users are far less common in 2026 and the
// crossplatform-shell-rcfile maze isn't worth boiling.

const (
	managedHeader = "# >>> devour managed block (do not edit between markers) >>>"
	managedFooter = "# <<< devour managed block <<<"
)

type darwinManager struct {
	paths config.Paths
	mu    sync.Mutex
}

func New(paths config.Paths) Manager { return &darwinManager{paths: paths} }

// rcPath is the file we write the managed block into. zsh on macOS
// reads ~/.zshrc for interactive shells. We don't write to .zprofile
// so IDE-launched terminals (which often skip login shells) still see
// our PATH additions.
func rcPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".zshrc")
}

// runtimeDef matches the Windows resolver's struct so the shared
// Manager interface returns identical RuntimeInfo on both platforms.
type runtimeDef struct {
	Name    string
	Label   string
	Subdir  string
	BinPath func(versionDir string) string
}

func runtimeDefs(installed string) []runtimeDef {
	return []runtimeDef{
		{Name: "php", Label: "PHP", Subdir: "php", BinPath: func(d string) string { return d }},
		{Name: "node", Label: "Node.js", Subdir: "node", BinPath: func(d string) string {
			// Node tarballs on macOS often extract to a node-vX.Y.Z-darwin-arm64/bin layout.
			if exe := findFileBelow(d, "node", 2); exe != "" {
				return filepath.Dir(exe)
			}
			return filepath.Join(d, "bin")
		}},
		{Name: "go", Label: "Go", Subdir: "go", BinPath: func(d string) string { return filepath.Join(d, "bin") }},
		{Name: "composer", Label: "Composer", Subdir: "composer", BinPath: func(d string) string { return d }},
		{Name: "mysql", Label: "MySQL", Subdir: "mysql", BinPath: func(d string) string {
			if exe := findFileBelow(d, "mysql", 2); exe != "" {
				return filepath.Dir(exe)
			}
			return filepath.Join(d, "bin")
		}},
		{Name: "postgresql", Label: "PostgreSQL", Subdir: "postgresql", BinPath: func(d string) string {
			if exe := findFileBelow(d, "psql", 3); exe != "" {
				return filepath.Dir(exe)
			}
			return filepath.Join(d, "bin")
		}},
	}
}

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
			if e.Name() == target {
				return filepath.Join(f.dir, e.Name())
			}
		}
	}
	return ""
}

// readRC returns ~/.zshrc contents. Empty string is fine if the file
// doesn't exist — first-time use just creates it with our block.
func readRC() string {
	rc := rcPath()
	if rc == "" {
		return ""
	}
	data, err := os.ReadFile(rc)
	if err != nil {
		return ""
	}
	return string(data)
}

// extractManagedPaths returns the PATH entries from inside our managed
// block (in order), and the rest of .zshrc with the managed block
// stripped out (so callers can rewrite it cleanly without disturbing
// non-Devour content).
func extractManagedPaths(rc string) (managed []string, withoutBlock string) {
	startIdx := strings.Index(rc, managedHeader)
	if startIdx < 0 {
		return nil, rc
	}
	endIdx := strings.Index(rc, managedFooter)
	if endIdx < 0 || endIdx <= startIdx {
		return nil, rc
	}

	block := rc[startIdx:endIdx+len(managedFooter)]
	for _, line := range strings.Split(block, "\n") {
		// Format we write: `export PATH="<dir>:$PATH"`
		if strings.HasPrefix(line, `export PATH="`) {
			rest := strings.TrimPrefix(line, `export PATH="`)
			if i := strings.Index(rest, `:$PATH"`); i >= 0 {
				managed = append(managed, rest[:i])
			}
		}
	}

	// Strip the block plus a single surrounding newline so we don't
	// accumulate blank lines on each rewrite.
	prefix := strings.TrimRight(rc[:startIdx], "\n")
	suffix := strings.TrimLeft(rc[endIdx+len(managedFooter):], "\n")
	withoutBlock = prefix
	if suffix != "" {
		if withoutBlock != "" {
			withoutBlock += "\n\n"
		}
		withoutBlock += suffix
	}
	return managed, withoutBlock
}

// writeRC composes a .zshrc with our managed block at the bottom and
// writes atomically. Each path becomes its own `export PATH=...` line
// so commenting one out manually is easy if the user wants to.
func writeRC(rest string, paths []string) error {
	rc := rcPath()
	if rc == "" {
		return fmt.Errorf("syspath: cannot resolve $HOME")
	}
	var sb strings.Builder
	sb.WriteString(strings.TrimRight(rest, "\n"))
	if rest != "" {
		sb.WriteString("\n\n")
	}
	sb.WriteString(managedHeader)
	sb.WriteString("\n# Managed by Devour. Edit via the PATH page; manual edits between\n")
	sb.WriteString("# the >>> and <<< markers will be overwritten.\n")
	for _, p := range paths {
		// Skip empty / duplicate lines defensively.
		if strings.TrimSpace(p) == "" {
			continue
		}
		fmt.Fprintf(&sb, "export PATH=\"%s:$PATH\"\n", p)
	}
	sb.WriteString(managedFooter)
	sb.WriteString("\n")
	return atomicfile.Write(rc, []byte(sb.String()), 0644)
}

// --- Manager interface implementation ---

func (m *darwinManager) List() ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked()
}

func (m *darwinManager) listLocked() ([]Entry, error) {
	managed, _ := extractManagedPaths(readRC())
	out := make([]Entry, 0, len(managed))
	installed := strings.ToLower(filepath.Clean(m.paths.InstalledPath()))
	for _, p := range managed {
		clean := filepath.Clean(p)
		entry := Entry{Path: p, Scope: ScopeUser}
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
	return out, nil
}

func (m *darwinManager) Add(path string, scope Scope) error {
	if scope == ScopeSystem {
		return fmt.Errorf("syspath: system PATH on macOS is /etc/paths — edit via Terminal with sudo")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	managed, rest := extractManagedPaths(readRC())
	for _, p := range managed {
		if filepath.Clean(p) == filepath.Clean(path) {
			return nil
		}
	}
	managed = append([]string{path}, managed...)
	return writeRC(rest, managed)
}

func (m *darwinManager) Remove(path string, scope Scope) error {
	if scope == ScopeSystem {
		return fmt.Errorf("syspath: system PATH not editable from Devour on macOS")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	managed, rest := extractManagedPaths(readRC())
	target := filepath.Clean(path)
	out := managed[:0]
	for _, p := range managed {
		if filepath.Clean(p) == target {
			continue
		}
		out = append(out, p)
	}
	return writeRC(rest, out)
}

func (m *darwinManager) SetActiveRuntime(runtime, version string) error {
	def := findRuntimeDef(runtime, m.paths.InstalledPath())
	if def == nil {
		return fmt.Errorf("syspath: unknown runtime %q", runtime)
	}
	versionDir := filepath.Join(m.paths.InstalledPath(), def.Subdir, version)
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		return fmt.Errorf("syspath: %s %s is not installed at %s", runtime, version, versionDir)
	}
	binPath := def.BinPath(versionDir)
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		return fmt.Errorf("syspath: bin dir for %s %s does not exist at %s", runtime, version, binPath)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	managed, rest := extractManagedPaths(readRC())
	runtimeRoot := strings.ToLower(filepath.Clean(filepath.Join(m.paths.InstalledPath(), def.Subdir)))
	out := make([]string, 0, len(managed)+1)
	for _, p := range managed {
		lower := strings.ToLower(filepath.Clean(p))
		if strings.HasPrefix(lower, runtimeRoot+string(filepath.Separator)) || lower == runtimeRoot {
			continue
		}
		out = append(out, p)
	}
	out = append([]string{binPath}, out...)
	return writeRC(rest, out)
}

func (m *darwinManager) GetRuntimes() ([]RuntimeInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.listLocked()
	if err != nil {
		return nil, err
	}
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
	for _, def := range runtimeDefs(m.paths.InstalledPath()) {
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

func findRuntimeDef(name, installed string) *runtimeDef {
	for _, d := range runtimeDefs(installed) {
		if d.Name == name {
			return &d
		}
	}
	return nil
}

// BroadcastChange is a no-op on macOS — there's no system-wide
// "environment changed" signal like Windows' WM_SETTINGCHANGE, and
// new terminal windows always re-read .zshrc anyway.
func (m *darwinManager) BroadcastChange() {}
