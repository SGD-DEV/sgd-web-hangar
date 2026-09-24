// Package bootstrap implements the first-run experience: detecting an empty
// install, presenting a default bundle of services, and orchestrating their
// download/extraction/activation through the existing packages.Manager.
package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/packages"
)

// BundleItem identifies a package version to install during bootstrap.
type BundleItem struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// BundleStatus is the per-item status emitted during bootstrap.
type BundleStatus struct {
	Name       string  `json:"name"`
	Version    string  `json:"version"`
	Label      string  `json:"label"`
	Status     string  `json:"status"` // pending | downloading | installed | active | error
	Percent    float64 `json:"percent"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	SpeedMBps  float64 `json:"speed_mbps"`
	ETA        int     `json:"eta_seconds"`
	Error      string  `json:"error,omitempty"`
}

// DefaultBundle returns the canonical first-run set — the latest stable
// version of every service a typical Laravel/PHP dev expects out of the box.
// Versions must match entries in app/packages/registry.go AllPackages();
// keep both in sync when bumping.
//
// All items are pre-checked in the wizard. PHP+Apache+MySQL are marked
// Required (the toggles are disabled) because Devour's project flow can't
// function without them. Everything else is optional but checked by default
// so the user gets a fully-loaded environment if they just hit Install.
//
// HeidiSQL is included here so the user has a working DB browser the first
// time they click "Open in HeidiSQL" — without bundling it, that button
// silently fails on a fresh install.
func DefaultBundle() []BundleItem {
	return []BundleItem{
		{Name: "php", Version: "8.3", Label: "PHP 8.3 (recommended)", Required: true},
		{Name: "apache", Version: "2.4.68", Label: "Apache 2.4.68", Required: true},
		{Name: "mysql", Version: "8.4", Label: "MySQL 8.4 LTS", Required: true},
		{Name: "nginx", Version: "1.29.1", Label: "Nginx 1.29.1", Required: false},
		{Name: "postgresql", Version: "18.3", Label: "PostgreSQL 18.3", Required: false},
		{Name: "heidisql", Version: "12.17", Label: "HeidiSQL 12.17 (DB GUI)", Required: false},
	}
}

// Manager wraps the packages.Manager to drive a serial bundle install with
// progress callbacks suitable for the WelcomeWizard UI.
type Manager struct {
	paths  config.Paths
	pkgs   *packages.Manager
	emitFn func(event string, data interface{})

	mu       sync.RWMutex
	statuses map[string]*BundleStatus
	running  bool
}

func NewManager(paths config.Paths, pkgs *packages.Manager, emitFn func(string, interface{})) *Manager {
	return &Manager{
		paths:    paths,
		pkgs:     pkgs,
		emitFn:   emitFn,
		statuses: make(map[string]*BundleStatus),
	}
}

// IsFirstRun returns true if no package directory under data/installed/
// contains an installed binary. This is the signal that the welcome wizard
// should appear.
func (m *Manager) IsFirstRun() bool {
	installed := m.paths.InstalledPath()
	for _, name := range []string{"php", "apache", "nginx", "mysql", "postgresql"} {
		dir := filepath.Join(installed, name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			versionDir := filepath.Join(dir, e.Name())
			if hasAny(versionDir) {
				return false
			}
		}
	}
	return true
}

// hasAny returns true if dir contains at least one file or subdirectory.
func hasAny(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// RunBootstrap installs the requested bundle items in sequence, then activates
// each one so service controllers can discover them. Progress is emitted via
// the emitFn callback under the event name "bootstrap:progress".
//
// Returns once all items reach a terminal state (installed/active or error).
func (m *Manager) RunBootstrap(items []BundleItem) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("bootstrap already running")
	}
	m.running = true
	for _, it := range items {
		m.statuses[key(it)] = &BundleStatus{
			Name: it.Name, Version: it.Version, Label: it.Label, Status: "pending",
		}
	}
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()

	for _, item := range items {
		m.update(item, "downloading", 0, "")
		if err := m.installAndWait(item); err != nil {
			m.update(item, "error", 0, err.Error())
			if item.Required {
				return fmt.Errorf("required package %s %s failed: %w", item.Name, item.Version, err)
			}
			continue
		}
		m.update(item, "installed", 100, "")

		// Activate so the service controllers can discover the binary.
		if err := m.pkgs.ActivatePackage(item.Name, item.Version); err != nil {
			m.update(item, "error", 100, fmt.Sprintf("activate failed: %v", err))
			if item.Required {
				return fmt.Errorf("activate %s %s: %w", item.Name, item.Version, err)
			}
			continue
		}
		m.update(item, "active", 100, "")
	}

	return nil
}

// installAndWait kicks off the package install and polls progress until it
// finishes (status = "complete" or "error"), bridging packages.Manager's
// async per-package channel to a synchronous step in the bootstrap sequence.
func (m *Manager) installAndWait(item BundleItem) error {
	if err := m.pkgs.InstallPackage(item.Name, item.Version); err != nil {
		// "already installed" is fine — treat as success.
		if isAlreadyInstalled(err) {
			return nil
		}
		return err
	}

	timeout := time.After(15 * time.Minute)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("download timed out")
		case <-tick.C:
			p := m.pkgs.GetDownloadProgress(item.Name, item.Version)
			if p == nil {
				continue
			}
			switch p.Status {
			case "complete":
				return nil
			case "error":
				return fmt.Errorf("%s", p.Error)
			default:
				m.updateProgress(item, p.Status, p.Percent, p.Downloaded, p.Total, p.SpeedMBps, p.ETA, p.Error)
			}
		}
	}
}

func (m *Manager) update(item BundleItem, status string, percent float64, errMsg string) {
	m.updateProgress(item, status, percent, 0, 0, 0, 0, errMsg)
}

// updateProgress is the full-fat version that also carries byte counters and
// speed/ETA. Used during the active downloading phase. State-only transitions
// (pending -> downloading -> installed -> active) go through update().
func (m *Manager) updateProgress(item BundleItem, status string, percent float64, downloaded, total int64, speedMBps float64, eta int, errMsg string) {
	m.mu.Lock()
	st := m.statuses[key(item)]
	if st == nil {
		st = &BundleStatus{Name: item.Name, Version: item.Version, Label: item.Label}
		m.statuses[key(item)] = st
	}
	st.Status = status
	st.Percent = percent
	st.Downloaded = downloaded
	st.Total = total
	st.SpeedMBps = speedMBps
	st.ETA = eta
	st.Error = errMsg
	snapshot := *st
	m.mu.Unlock()

	if m.emitFn != nil {
		m.emitFn("bootstrap:progress", snapshot)
	}
}

// Status returns the current snapshot of all bundle items.
func (m *Manager) Status() []BundleStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]BundleStatus, 0, len(m.statuses))
	for _, s := range m.statuses {
		out = append(out, *s)
	}
	return out
}

func key(item BundleItem) string {
	return item.Name + "-" + item.Version
}

func isAlreadyInstalled(err error) bool {
	return err != nil && stringContains(err.Error(), "already installed")
}

func stringContains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
