//go:build linux

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

// Linux PATH management — same managed-block strategy as macOS, just
// targeting bash as the more common shell on Linux. Honors $SHELL: if
// the user is on zsh we write ~/.zshrc; otherwise ~/.bashrc.
//
// New shells pick up the change on next launch (sourcing the rcfile).
// No equivalent of WM_SETTINGCHANGE on Linux either, so already-open
// shells need to re-source manually — same UX as macOS.

const (
	managedHeader = "# >>> devour managed block (do not edit between markers) >>>"
	managedFooter = "# <<< devour managed block <<<"
)

type linuxManager struct {
	paths config.Paths
	mu    sync.Mutex
}

func New(paths config.Paths) Manager { return &linuxManager{paths: paths} }

// rcPath returns the rc file we write into. Picks ~/.zshrc when the
// user's $SHELL ends in zsh, otherwise ~/.bashrc. We don't try fish,
// nu, etc. — those users typically know how to edit their own config.
func rcPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	shell := os.Getenv("SHELL")
	if strings.HasSuffix(shell, "zsh") {
		return filepath.Join(home, ".zshrc")
	}
	return filepath.Join(home, ".bashrc")
}

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

func extractManagedPaths(rc string) (managed []string, withoutBlock string) {
	startIdx := strings.Index(rc, managedHeader)
	if startIdx < 0 {
		return nil, rc
	}
	endIdx := strings.Index(rc, managedFooter)
	if endIdx < 0 || endIdx <= startIdx {
		return nil, rc
	}

	block := rc[startIdx : endIdx+len(managedFooter)]
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, `export PATH="`) {
			rest := strings.TrimPrefix(line, `export PATH="`)
			if i := strings.Index(rest, `:$PATH"`); i >= 0 {
				managed = append(managed, rest[:i])
			}
		}
	}

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

func (m *linuxManager) List() ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked()
}

func (m *linuxManager) listLocked() ([]Entry, error) {
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

func (m *linuxManager) Add(path string, scope Scope) error {
	if scope == ScopeSystem {
		return fmt.Errorf("syspath: system PATH on Linux lives in /etc — edit /etc/environment with sudo")
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

func (m *linuxManager) Remove(path string, scope Scope) error {
	if scope == ScopeSystem {
		return fmt.Errorf("syspath: system PATH not editable from Devour on Linux")
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

func (m *linuxManager) SetActiveRuntime(runtime, version string) error {
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

func (m *linuxManager) GetRuntimes() ([]RuntimeInfo, error) {
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

func (m *linuxManager) BroadcastChange() {}
