package php

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devour-app/devour/app/config"
)

type VersionInfo struct {
	Version   string `json:"version"`
	Path      string `json:"path"`
	IsActive  bool   `json:"is_active"`
	IsDefault bool   `json:"is_default"`
}

type IniSettings struct {
	MemoryLimit       string   `json:"memory_limit"`
	UploadMaxFilesize string   `json:"upload_max_filesize"`
	PostMaxSize       string   `json:"post_max_size"`
	MaxExecutionTime  string   `json:"max_execution_time"`
	MaxInputTime      string   `json:"max_input_time"`
	DisplayErrors     string   `json:"display_errors"`
	ErrorReporting    string   `json:"error_reporting"`
	Extensions        []string `json:"extensions"`
	Timezone          string   `json:"timezone"`
}

type Manager struct {
	paths config.Paths
	store *config.Store
}

func NewManager(paths config.Paths, store *config.Store) *Manager {
	return &Manager{
		paths: paths,
		store: store,
	}
}

func (m *Manager) ListInstalled() []VersionInfo {
	phpDir := filepath.Join(m.paths.InstalledPath(), "php")
	entries, err := os.ReadDir(phpDir)
	if err != nil {
		return nil
	}

	activeVersion := m.ActiveVersion()
	var versions []VersionInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		phpExe := filepath.Join(phpDir, entry.Name(), "php.exe")
		if _, err := os.Stat(phpExe); err != nil {
			continue
		}
		versions = append(versions, VersionInfo{
			Version:   entry.Name(),
			Path:      filepath.Join(phpDir, entry.Name()),
			IsActive:  entry.Name() == activeVersion,
			IsDefault: entry.Name() == activeVersion,
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i].Version, versions[j].Version) > 0
	})

	return versions
}

func (m *Manager) ActiveVersion() string {
	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return ""
	}
	return cfg.ActivePHP
}

func (m *Manager) SwitchGlobal(version string) error {
	phpPath := m.paths.PHPPath(version)
	phpExe := filepath.Join(phpPath, "php.exe")
	if _, err := os.Stat(phpExe); os.IsNotExist(err) {
		return fmt.Errorf("php: version %s not installed at %s", version, phpPath)
	}

	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return fmt.Errorf("php: reading config: %w", err)
	}

	cfg.ActivePHP = version
	if err := m.store.SaveAppConfig(cfg); err != nil {
		return fmt.Errorf("php: saving config: %w", err)
	}

	return nil
}

func (m *Manager) SwitchForProject(version, projectName string) error {
	phpPath := m.paths.PHPPath(version)
	phpExe := filepath.Join(phpPath, "php.exe")
	if _, err := os.Stat(phpExe); os.IsNotExist(err) {
		return fmt.Errorf("php: version %s not installed", version)
	}

	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return fmt.Errorf("php: reading config: %w", err)
	}

	if cfg.PHPPerProject == nil {
		cfg.PHPPerProject = make(map[string]string)
	}
	cfg.PHPPerProject[projectName] = version

	if err := m.store.SaveAppConfig(cfg); err != nil {
		return fmt.Errorf("php: saving config: %w", err)
	}

	return nil
}

func (m *Manager) GetProjectPHP(projectName string) string {
	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return ""
	}
	if v, ok := cfg.PHPPerProject[projectName]; ok {
		return v
	}
	return cfg.ActivePHP
}

func (m *Manager) GetIniSettings(version string) (IniSettings, error) {
	iniMgr := NewIniManager(m.paths)
	return iniMgr.Read(version)
}

func (m *Manager) UpdateIniSettings(version string, settings IniSettings) error {
	iniMgr := NewIniManager(m.paths)
	return iniMgr.Write(version, settings)
}

func (m *Manager) GetPHPExePath(version string) string {
	return filepath.Join(m.paths.PHPPath(version), "php.exe")
}

type ExtensionInfo struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// GetExtensions lists all available PHP extensions for a version by scanning the ext/ directory
// and checking php.ini for which ones are enabled.
func (m *Manager) GetExtensions(version string) []ExtensionInfo {
	phpDir := m.paths.PHPPath(version)
	extDir := filepath.Join(phpDir, "ext")

	entries, err := os.ReadDir(extDir)
	if err != nil {
		return nil
	}

	// Collect all available .dll extensions
	available := make(map[string]bool)
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(name), ".dll") && strings.HasPrefix(name, "php_") {
			extName := strings.TrimSuffix(strings.TrimPrefix(name, "php_"), ".dll")
			available[extName] = false
		}
	}

	// Parse php.ini to find enabled/disabled extensions
	iniMgr := NewIniManager(m.paths)
	iniPath := iniMgr.findIniPath(version)
	if iniPath != "" {
		f, err := os.Open(iniPath)
		if err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				enabled := false
				var ext string
				switch {
				case strings.HasPrefix(line, "zend_extension="):
					ext, enabled = strings.TrimPrefix(line, "zend_extension="), true
				case strings.HasPrefix(line, ";zend_extension="):
					ext = strings.TrimPrefix(line, ";zend_extension=")
				case strings.HasPrefix(line, "extension="):
					ext, enabled = strings.TrimPrefix(line, "extension="), true
				case strings.HasPrefix(line, ";extension="):
					ext = strings.TrimPrefix(line, ";extension=")
				default:
					continue
				}
				ext = strings.Trim(ext, "\"' ")
				ext = strings.TrimSuffix(ext, ".dll")
				ext = strings.TrimPrefix(ext, "php_")
				if ext == "" {
					continue
				}
				if _, ok := available[ext]; ok {
					if enabled {
						available[ext] = true
					}
				}
			}
			f.Close()
		}
	}

	var result []ExtensionInfo
	for name, enabled := range available {
		result = append(result, ExtensionInfo{Name: name, Enabled: enabled})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// zendExtensions must be loaded with zend_extension=, not extension=
// (PHP warns "Invalid library (appears to be a Zend Extension)").
var zendExtensions = map[string]bool{"opcache": true, "xdebug": true}

// ToggleExtension enables or disables a PHP extension in php.ini
func (m *Manager) ToggleExtension(version, extName string, enable bool) error {
	iniMgr := NewIniManager(m.paths)
	iniPath := iniMgr.findIniPath(version)
	if iniPath == "" {
		// Create php.ini from php.ini-production if it doesn't exist
		phpDir := m.paths.PHPPath(version)
		iniPath = filepath.Join(phpDir, "php.ini")
		prodIni := filepath.Join(phpDir, "php.ini-production")
		if _, err := os.Stat(prodIni); err == nil {
			data, _ := os.ReadFile(prodIni)
			os.WriteFile(iniPath, data, 0644)
		} else {
			os.WriteFile(iniPath, []byte(""), 0644)
		}
	}

	data, err := os.ReadFile(iniPath)
	if err != nil {
		return fmt.Errorf("php: reading ini: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	directive := "extension"
	if zendExtensions[extName] {
		directive = "zend_extension"
	}
	extLine := fmt.Sprintf("%s=%s", directive, extName)
	commentedLine := ";" + extLine

	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if directive == "zend_extension" && (trimmed == "extension="+extName || trimmed == "extension=php_"+extName+".dll") {
			// Written by an older Hangar with the wrong directive.
			lines[i] = ";" + trimmed + " ; wrong directive, loaded via zend_extension"
			continue
		}
		// Match "extension=extName" or ";extension=extName"
		if trimmed == extLine || trimmed == commentedLine ||
			trimmed == "extension=php_"+extName+".dll" || trimmed == ";extension=php_"+extName+".dll" ||
			trimmed == "extension=php_"+extName || trimmed == ";extension=php_"+extName {
			found = true
			if enable {
				lines[i] = extLine
			} else {
				lines[i] = ";" + extLine
			}
		}
	}

	if !found && enable {
		if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, extLine)
	}

	return os.WriteFile(iniPath, []byte(strings.Join(lines, "\n")), 0644)
}

func compareVersions(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	for i := 0; i < maxLen; i++ {
		var numA, numB int
		if i < len(partsA) {
			numA = parseNum(partsA[i])
		}
		if i < len(partsB) {
			numB = parseNum(partsB[i])
		}
		if numA != numB {
			return numA - numB
		}
	}
	return 0
}

func parseNum(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			break
		}
	}
	return n
}
