package php

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devour-app/devour/app/config"
)

type IniManager struct {
	paths config.Paths
}

func NewIniManager(paths config.Paths) *IniManager {
	return &IniManager{paths: paths}
}

func (im *IniManager) Read(version string) (IniSettings, error) {
	iniPath := im.findIniPath(version)
	if iniPath == "" {
		return IniSettings{}, fmt.Errorf("php ini: no php.ini found for version %s", version)
	}

	f, err := os.Open(iniPath)
	if err != nil {
		return IniSettings{}, fmt.Errorf("php ini: opening %s: %w", iniPath, err)
	}
	defer f.Close()

	settings := IniSettings{}
	var extensions []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "extension=") {
			ext := strings.TrimPrefix(line, "extension=")
			ext = strings.Trim(ext, "\"' ")
			extensions = append(extensions, ext)
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "memory_limit":
			settings.MemoryLimit = value
		case "upload_max_filesize":
			settings.UploadMaxFilesize = value
		case "post_max_size":
			settings.PostMaxSize = value
		case "max_execution_time":
			settings.MaxExecutionTime = value
		case "max_input_time":
			settings.MaxInputTime = value
		case "display_errors":
			settings.DisplayErrors = value
		case "error_reporting":
			settings.ErrorReporting = value
		case "date.timezone":
			settings.Timezone = value
		}
	}

	settings.Extensions = extensions
	return settings, scanner.Err()
}

func (im *IniManager) Write(version string, settings IniSettings) error {
	iniPath := im.findIniPath(version)
	if iniPath == "" {
		iniPath = filepath.Join(im.paths.PHPPath(version), "php.ini")
		prodIni := filepath.Join(im.paths.PHPPath(version), "php.ini-production")
		if _, err := os.Stat(prodIni); err == nil {
			data, err := os.ReadFile(prodIni)
			if err == nil {
				os.WriteFile(iniPath, data, 0644)
			}
		}
	}

	f, err := os.Open(iniPath)
	if err != nil {
		return fmt.Errorf("php ini: opening %s: %w", iniPath, err)
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	f.Close()

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("php ini: reading %s: %w", iniPath, err)
	}

	settingsMap := map[string]string{
		"memory_limit":       settings.MemoryLimit,
		"upload_max_filesize": settings.UploadMaxFilesize,
		"post_max_size":      settings.PostMaxSize,
		"max_execution_time": settings.MaxExecutionTime,
		"max_input_time":     settings.MaxInputTime,
		"display_errors":     settings.DisplayErrors,
		"error_reporting":    settings.ErrorReporting,
		"date.timezone":      settings.Timezone,
	}

	updated := make(map[string]bool)
	var newLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "extension=") {
			continue
		}

		matched := false
		for key, value := range settingsMap {
			if value == "" {
				continue
			}
			prefix := key + " "
			prefixEq := key + "="
			if strings.HasPrefix(trimmed, prefix) || strings.HasPrefix(trimmed, prefixEq) ||
				strings.HasPrefix(trimmed, ";"+prefix) || strings.HasPrefix(trimmed, ";"+prefixEq) {
				newLines = append(newLines, fmt.Sprintf("%s = %s", key, value))
				updated[key] = true
				matched = true
				break
			}
		}

		if !matched {
			newLines = append(newLines, line)
		}
	}

	for key, value := range settingsMap {
		if value != "" && !updated[key] {
			newLines = append(newLines, fmt.Sprintf("%s = %s", key, value))
		}
	}

	for _, ext := range settings.Extensions {
		newLines = append(newLines, fmt.Sprintf("extension=%s", ext))
	}

	return os.WriteFile(iniPath, []byte(strings.Join(newLines, "\n")), 0644)
}

func (im *IniManager) ApplyToAll(settings IniSettings) error {
	phpDir := filepath.Join(im.paths.InstalledPath(), "php")
	entries, err := os.ReadDir(phpDir)
	if err != nil {
		return fmt.Errorf("php ini: reading php dir: %w", err)
	}

	var firstErr error
	for _, entry := range entries {
		if entry.IsDir() {
			if err := im.Write(entry.Name(), settings); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("php ini: applying to %s: %w", entry.Name(), err)
			}
		}
	}
	return firstErr
}

func (im *IniManager) findIniPath(version string) string {
	phpPath := im.paths.PHPPath(version)

	candidates := []string{
		filepath.Join(phpPath, "php.ini"),
		filepath.Join(phpPath, "php.ini-development"),
		filepath.Join(phpPath, "php.ini-production"),
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}
