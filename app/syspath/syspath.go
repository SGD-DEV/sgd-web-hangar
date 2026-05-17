// Package syspath manages the user's Windows PATH environment variable from
// inside Devour, replacing the System Properties → Environment Variables dance
// (open dialog, edit PATH, restart terminal). It writes to HKCU so no admin
// elevation is needed; new terminals see the change immediately after the
// WM_SETTINGCHANGE broadcast.
package syspath

// Scope is "user" (HKCU\Environment, no admin) or "system" (HKLM, admin).
type Scope string

const (
	ScopeUser   Scope = "user"
	ScopeSystem Scope = "system"
)

// Entry is a single path on the user's PATH variable, annotated with whether
// Devour recognises it as one of its managed runtimes.
type Entry struct {
	Path    string `json:"path"`
	Scope   Scope  `json:"scope"`
	Managed bool   `json:"managed"`           // true if path lives under data/installed/
	Runtime string `json:"runtime,omitempty"` // php, node, composer, go, mysql, postgresql
	Version string `json:"version,omitempty"` // detected version segment, e.g. "8.3"
	Exists  bool   `json:"exists"`            // false if path no longer resolves on disk
}

// RuntimeInfo describes one runtime the user can swap in/out of PATH.
type RuntimeInfo struct {
	Name             string   `json:"name"`               // e.g. "php"
	Label            string   `json:"label"`              // human-friendly
	ActiveVersion    string   `json:"active_version"`     // version currently on PATH (empty if none)
	ActivePath       string   `json:"active_path"`        // the PATH entry that's active
	InstalledVersions []string `json:"installed_versions"` // versions Devour has on disk
}

// Manager is the OS-agnostic interface for reading/writing PATH and runtime
// activation. Each platform supplies its own implementation; non-Windows is
// currently a stub since Devour is Windows-first.
type Manager interface {
	List() ([]Entry, error)
	Add(path string, scope Scope) error
	Remove(path string, scope Scope) error
	SetActiveRuntime(runtime, version string) error
	GetRuntimes() ([]RuntimeInfo, error)
	BroadcastChange()
}
