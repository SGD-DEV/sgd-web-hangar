package php

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/devour-app/devour/app/config"
)

type Switcher struct {
	paths config.Paths
	store *config.Store
}

func NewSwitcher(paths config.Paths, store *config.Store) *Switcher {
	return &Switcher{
		paths: paths,
		store: store,
	}
}

func (s *Switcher) SwitchGlobal(version string) error {
	phpPath := s.paths.PHPPath(version)
	if _, err := os.Stat(filepath.Join(phpPath, "php.exe")); os.IsNotExist(err) {
		return fmt.Errorf("php switcher: version %s not installed", version)
	}

	if err := s.updateSystemPath(phpPath); err != nil {
		return fmt.Errorf("php switcher: updating PATH: %w", err)
	}

	if err := s.updateApacheModule(version); err != nil {
		return fmt.Errorf("php switcher: updating Apache module: %w", err)
	}

	return nil
}

func (s *Switcher) updateSystemPath(phpPath string) error {
	currentPath := os.Getenv("PATH")
	paths := strings.Split(currentPath, ";")

	var newPaths []string
	phpInstallBase := filepath.Join(s.paths.InstalledPath(), "php")

	for _, p := range paths {
		if !strings.HasPrefix(strings.ToLower(p), strings.ToLower(phpInstallBase)) {
			newPaths = append(newPaths, p)
		}
	}

	newPaths = append([]string{phpPath}, newPaths...)
	newPathStr := strings.Join(newPaths, ";")

	return os.Setenv("PATH", newPathStr)
}

func (s *Switcher) updateApacheModule(phpVersion string) error {
	phpPath := s.paths.PHPPath(phpVersion)

	modulePath := filepath.Join(phpPath, "php8apache2_4.dll")
	if _, err := os.Stat(modulePath); os.IsNotExist(err) {
		modulePath = filepath.Join(phpPath, "php7apache2_4.dll")
		if _, err := os.Stat(modulePath); os.IsNotExist(err) {
			return nil
		}
	}

	confPath := filepath.Join(s.paths.ConfPath("apache"), "httpd.conf")
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(confPath)
	if err != nil {
		return fmt.Errorf("reading httpd.conf: %w", err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	var newLines []string
	found := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "LoadModule php") && strings.Contains(trimmed, "module") {
			newLines = append(newLines, fmt.Sprintf(`LoadModule php_module "%s"`, filepath.ToSlash(modulePath)))
			found = true
		} else {
			newLines = append(newLines, line)
		}
	}

	if !found {
		newLines = append(newLines, fmt.Sprintf(`LoadModule php_module "%s"`, filepath.ToSlash(modulePath)))
	}

	return os.WriteFile(confPath, []byte(strings.Join(newLines, "\n")), 0644)
}

func (s *Switcher) GetCurrentPHPVersion() (string, error) {
	cfg, err := s.store.GetAppConfig()
	if err != nil {
		return "", err
	}
	if cfg.ActivePHP == "" {
		return "", fmt.Errorf("php switcher: no active PHP version set")
	}
	return cfg.ActivePHP, nil
}

func (s *Switcher) VerifyPHP(version string) (string, error) {
	phpExe := filepath.Join(s.paths.PHPPath(version), "php.exe")
	cmd := exec.Command(phpExe, "-v")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("php switcher: verifying %s: %w", version, err)
	}
	return strings.TrimSpace(string(output)), nil
}
