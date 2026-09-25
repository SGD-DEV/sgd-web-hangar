package packages

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/downloader"
)

type InstallStatus string

const (
	InstallStatusAvailable   InstallStatus = "available"
	InstallStatusDownloading InstallStatus = "downloading"
	InstallStatusInstalled   InstallStatus = "installed"
	InstallStatusActive      InstallStatus = "active"
	InstallStatusError       InstallStatus = "error"
)

type PackageInfo struct {
	Name        string        `json:"name"`
	Label       string        `json:"label"`
	Version     string        `json:"version"`
	Category    Category      `json:"category"`
	Status      InstallStatus `json:"status"`
	InstallPath string        `json:"install_path,omitempty"`
	IsActive    bool          `json:"is_active"`
}

type DownloadProgress struct {
	Name       string  `json:"name"`
	Version    string  `json:"version"`
	Percent    float64 `json:"percent"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	SpeedMBps  float64 `json:"speed_mbps"`
	ETA        int     `json:"eta_seconds"`
	Status     string  `json:"status"`
	Error      string  `json:"error,omitempty"`
}

type Manager struct {
	paths      config.Paths
	store      *config.Store
	dl         *downloader.Downloader
	mu         sync.RWMutex
	progress   map[string]*DownloadProgress
	progressMu sync.RWMutex
	emitFn     func(event string, data interface{})
}

func NewManager(paths config.Paths, store *config.Store, emitFn func(string, interface{})) *Manager {
	return &Manager{
		paths:    paths,
		store:    store,
		dl:       downloader.New(),
		progress: make(map[string]*DownloadProgress),
		emitFn:   emitFn,
	}
}

func (m *Manager) GetCategories() []CategoryInfo {
	return Categories()
}

func (m *Manager) GetPackages(category string) []PackageInfo {
	// Always include user-added custom packages alongside the built-in
	// registry. Custom entries take priority on a name+version collision so
	// users can override a registry URL (e.g. point php 8.3 at a different
	// mirror).
	all := mergePackages(AllPackages(), m.customPackages())

	var entries []PackageEntry
	if category == "" || category == "all" {
		entries = all
	} else {
		entries = filterByCategory(all, Category(category))
	}

	var result []PackageInfo
	for _, e := range entries {
		info := PackageInfo{
			Name:     e.Name,
			Label:    e.Label,
			Version:  e.Version,
			Category: e.Category,
			Status:   m.getInstallStatus(e),
			IsActive: m.isActive(e.Name, e.Version),
		}
		installDir := filepath.Join(m.paths.InstalledPath(), e.SubDir)
		if info.Status == InstallStatusInstalled || info.Status == InstallStatusActive {
			info.InstallPath = installDir
		}
		result = append(result, info)
	}
	return result
}

func (m *Manager) InstallPackage(name, version string) error {
	// Look in custom packages first, then the built-in registry. This lets a
	// user override a registry URL (e.g. point php 8.3 at a different mirror)
	// or install something the registry doesn't yet know about.
	pkg := m.findPackage(name, version)
	if pkg == nil {
		return fmt.Errorf("packages: %s %s not found in registry", name, version)
	}

	targetDir := filepath.Join(m.paths.InstalledPath(), pkg.SubDir)

	if isInstalled(targetDir, name) {
		return fmt.Errorf("packages: %s %s is already installed", name, version)
	}

	key := name + "-" + version
	m.progressMu.Lock()
	m.progress[key] = &DownloadProgress{
		Name:    name,
		Version: version,
		Status:  "downloading",
	}
	m.progressMu.Unlock()

	go func() {
		dl := downloader.New()

		go func() {
			for p := range dl.ProgressChan() {
				dp := &DownloadProgress{
					Name:       name,
					Version:    version,
					Percent:    p.Percent,
					Downloaded: p.Downloaded,
					Total:      p.Total,
					SpeedMBps:  p.SpeedMBps,
					ETA:        p.ETA,
					Status:     p.Status,
					Error:      p.Error,
				}
				m.progressMu.Lock()
				m.progress[key] = dp
				m.progressMu.Unlock()

				if m.emitFn != nil {
					m.emitFn("package:progress", dp)
				}
			}
		}()

		err := dl.Download(downloader.Request{
			Name:      pkg.Label,
			URL:       pkg.URL,
			TargetDir: targetDir,
		})

		if err != nil {
			dp := &DownloadProgress{
				Name:    name,
				Version: version,
				Status:  "error",
				Error:   err.Error(),
			}
			m.progressMu.Lock()
			m.progress[key] = dp
			m.progressMu.Unlock()
			if m.emitFn != nil {
				m.emitFn("package:progress", dp)
			}
			return
		}

		dp := &DownloadProgress{
			Name:    name,
			Version: version,
			Percent: 100,
			Status:  "complete",
		}
		m.progressMu.Lock()
		m.progress[key] = dp
		m.progressMu.Unlock()
		if m.emitFn != nil {
			m.emitFn("package:progress", dp)
		}
	}()

	return nil
}

func (m *Manager) RemovePackage(name, version string) error {
	pkg := m.findPackage(name, version)
	if pkg == nil {
		return fmt.Errorf("packages: %s %s not found in registry", name, version)
	}

	targetDir := filepath.Join(m.paths.InstalledPath(), pkg.SubDir)
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return fmt.Errorf("packages: %s %s is not installed", name, version)
	}

	if m.isActive(name, version) {
		return fmt.Errorf("packages: cannot remove active %s %s — deactivate it first", name, version)
	}

	return os.RemoveAll(targetDir)
}

func (m *Manager) ActivatePackage(name, version string) error {
	pkg := FindPackage(name, version)
	if pkg == nil {
		return fmt.Errorf("packages: %s %s not found", name, version)
	}

	targetDir := filepath.Join(m.paths.InstalledPath(), pkg.SubDir)
	if !isInstalled(targetDir, name) {
		return fmt.Errorf("packages: %s %s is not installed", name, version)
	}

	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return fmt.Errorf("packages: reading config: %w", err)
	}

	switch name {
	case "php":
		cfg.ActivePHP = version
	}

	if cfg.ActiveVersions == nil {
		cfg.ActiveVersions = make(map[string]string)
	}
	cfg.ActiveVersions[name] = version

	if err := m.store.SaveAppConfig(cfg); err != nil {
		return fmt.Errorf("packages: saving app config: %w", err)
	}

	// For service-type packages, also save the ServiceConfig so that service
	// find* functions can locate the binaries without fallback directory scanning.
	servicePort := 0
	switch name {
	case "apache":
		servicePort = cfg.ApachePort
		if servicePort == 0 {
			servicePort = 80
		}
	case "nginx":
		servicePort = cfg.NginxPort
		if servicePort == 0 {
			servicePort = 8080
		}
	case "mysql":
		servicePort = cfg.MySQLPort
		if servicePort == 0 {
			servicePort = 3306
		}
	case "postgresql":
		servicePort = cfg.PostgreSQLPort
		if servicePort == 0 {
			servicePort = 5432
		}
	}
	if servicePort > 0 {
		svcCfg := config.ServiceConfig{
			Name:        name,
			Version:     version,
			InstallPath: targetDir,
			Port:        servicePort,
			Enabled:     true,
		}
		if name == "mysql" || name == "postgresql" {
			svcCfg.DataDir = filepath.Join(m.paths.DataPath(), name+"-data", version)
			if name == "postgresql" {
				svcCfg.DataDir = filepath.Join(m.paths.DataPath(), "pg-data", version)
			}
		}
		if err := m.store.SaveServiceConfig(name, svcCfg); err != nil {
			return fmt.Errorf("packages: saving service config for %s: %w", name, err)
		}
	}

	return nil
}

func (m *Manager) GetDownloadProgress(name, version string) *DownloadProgress {
	key := name + "-" + version
	m.progressMu.RLock()
	defer m.progressMu.RUnlock()
	if p, ok := m.progress[key]; ok {
		return p
	}
	return nil
}

func (m *Manager) GetAllProgress() map[string]*DownloadProgress {
	m.progressMu.RLock()
	defer m.progressMu.RUnlock()
	result := make(map[string]*DownloadProgress, len(m.progress))
	for k, v := range m.progress {
		result[k] = v
	}
	return result
}

func (m *Manager) getInstallStatus(pkg PackageEntry) InstallStatus {
	targetDir := filepath.Join(m.paths.InstalledPath(), pkg.SubDir)

	if !isInstalled(targetDir, pkg.Name) {
		key := pkg.Name + "-" + pkg.Version
		m.progressMu.RLock()
		if p, ok := m.progress[key]; ok && (p.Status == "downloading" || p.Status == "extracting" || p.Status == "verifying") {
			m.progressMu.RUnlock()
			return InstallStatusDownloading
		}
		m.progressMu.RUnlock()
		return InstallStatusAvailable
	}

	if m.isActive(pkg.Name, pkg.Version) {
		return InstallStatusActive
	}

	return InstallStatusInstalled
}

// IsActive reports whether name@version is the active version.
func (m *Manager) IsActive(name, version string) bool { return m.isActive(name, version) }

// InstalledVersions lists the installed versions of a package.
func (m *Manager) InstalledVersions(name string) []string {
	var out []string
	for _, p := range m.GetPackages("") {
		if p.Name == name && (p.Status == InstallStatusInstalled || p.Status == InstallStatusActive) {
			out = append(out, p.Version)
		}
	}
	return out
}

// Deactivate clears the active marker of name if it points at version, so
// the package can be removed.
func (m *Manager) Deactivate(name, version string) error {
	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return err
	}
	changed := false
	if cfg.ActiveVersions != nil && cfg.ActiveVersions[name] == version {
		delete(cfg.ActiveVersions, name)
		changed = true
	}
	if name == "php" && cfg.ActivePHP == version {
		cfg.ActivePHP = ""
		changed = true
	}
	if !changed {
		return nil
	}
	return m.store.SaveAppConfig(cfg)
}

func (m *Manager) isActive(name, version string) bool {
	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return false
	}

	if name == "php" && cfg.ActivePHP == version {
		return true
	}

	if cfg.ActiveVersions != nil {
		if v, ok := cfg.ActiveVersions[name]; ok && v == version {
			return true
		}
	}

	return false
}

func isInstalled(dir, name string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return false
	}

	switch name {
	case "php":
		return fileExists(dir, "php.exe")
	case "apache":
		return fileExists(dir, "Apache24", "bin", "httpd.exe") || findExeRecursive(dir, "httpd.exe")
	case "nginx":
		return fileExists(dir, "nginx.exe") || findExeRecursive(dir, "nginx.exe")
	case "mysql":
		return fileExists(dir, "bin", "mysqld.exe") || findExeRecursive(dir, "mysqld.exe")
	case "postgresql":
		return fileExists(dir, "pgsql", "bin", "postgres.exe") || findExeRecursive(dir, "postgres.exe")
	case "node":
		return fileExists(dir, "node.exe") || findExeRecursive(dir, "node.exe")
	case "heidisql":
		return fileExists(dir, "heidisql.exe") || findExeRecursive(dir, "heidisql.exe")
	case "composer":
		return fileExists(dir, "composer.phar")
	default:
		return len(entries) > 0
	}
}

func fileExists(parts ...string) bool {
	p := filepath.Join(parts...)
	_, err := os.Stat(p)
	return err == nil
}

func findExeRecursive(dir, target string) bool {
	found := false
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.EqualFold(info.Name(), target) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// --- Custom (user-added) packages -------------------------------------------
//
// These persist in the BoltDB store under bucketCustomPackages, keyed by
// "<name>-<version>". On every GetPackages call we merge them with the
// built-in registry. AddCustomPackage is what the "Add Package URL" form
// in the UI calls; URLDetect (in detect.go) prefills the form fields from
// the URL's host so users don't have to type out name/version/category.

// customPackages reads all user-added packages out of the store. Returns an
// empty slice on any error so a corrupt store doesn't take down the rest of
// the package list.
func (m *Manager) customPackages() []PackageEntry {
	if m.store == nil {
		return nil
	}
	all, err := m.store.GetAllCustomPackages()
	if err != nil {
		return nil
	}
	out := make([]PackageEntry, 0, len(all))
	for _, raw := range all {
		var p PackageEntry
		if err := json.Unmarshal(raw, &p); err == nil && p.Name != "" {
			out = append(out, p)
		}
	}
	return out
}

// findPackage looks up a package by name+version, checking custom entries
// first (so users can override registry URLs) then the built-in registry.
func (m *Manager) findPackage(name, version string) *PackageEntry {
	for _, p := range m.customPackages() {
		if p.Name == name && p.Version == version {
			return &p
		}
	}
	return FindPackage(name, version)
}

// mergePackages combines the built-in registry with custom packages. On a
// name+version collision, the custom entry wins.
func mergePackages(builtin, custom []PackageEntry) []PackageEntry {
	type k struct{ name, version string }
	idx := make(map[k]int, len(builtin))
	out := append([]PackageEntry{}, builtin...)
	for i, p := range out {
		idx[k{p.Name, p.Version}] = i
	}
	for _, p := range custom {
		key := k{p.Name, p.Version}
		if i, ok := idx[key]; ok {
			out[i] = p // override
		} else {
			out = append(out, p)
		}
	}
	return out
}

// filterByCategory returns the subset of entries matching cat. Replaces the
// package-level PackagesByCategory which only knows about the built-in
// registry.
func filterByCategory(entries []PackageEntry, cat Category) []PackageEntry {
	var out []PackageEntry
	for _, p := range entries {
		if p.Category == cat {
			out = append(out, p)
		}
	}
	return out
}

// AddCustomPackage persists a new custom package entry. The frontend calls
// this after the user fills in the "Add Package URL" form (auto-prefilled
// from URLDetect when available).
func (m *Manager) AddCustomPackage(p PackageEntry) error {
	if p.Name == "" || p.Version == "" || p.URL == "" {
		return fmt.Errorf("packages: name, version, and URL are required")
	}
	if p.SubDir == "" {
		p.SubDir = p.Name + "/" + p.Version
	}
	if p.Label == "" {
		p.Label = p.Name + " " + p.Version
	}
	if p.Category == "" {
		p.Category = CategoryTools
	}
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("packages: marshaling: %w", err)
	}
	key := p.Name + "-" + p.Version
	return m.store.SaveCustomPackage(key, data)
}

// RemoveCustomPackage deletes a user-added entry from the store. Does NOT
// delete the on-disk install - users can call RemovePackage(name, version)
// for that. Removing only the registry entry is useful for cleaning up
// experimental URLs without uninstalling the binaries.
func (m *Manager) RemoveCustomPackage(name, version string) error {
	return m.store.DeleteCustomPackage(name + "-" + version)
}

// ListCustomPackages returns every user-added entry. Used by the Packages
// page to render a "Custom" section above the built-in registry, with
// edit/delete affordances.
func (m *Manager) ListCustomPackages() []PackageEntry {
	return m.customPackages()
}
