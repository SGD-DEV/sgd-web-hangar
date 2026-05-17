package services

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed assets/index.php
var welcomePageTmpl string

// EnsureWelcomePage copies the embedded welcome page to the runtime www directory,
// replacing the {{PROJECTS_PATH}} placeholder with the real projects path.
// It only writes if the file does not already exist (so users can customize it).
func EnsureWelcomePage(wwwDir, projectsPath string) error {
	if err := os.MkdirAll(wwwDir, 0755); err != nil {
		return fmt.Errorf("creating www dir: %w", err)
	}

	target := filepath.Join(wwwDir, "index.php")
	if _, err := os.Stat(target); err == nil {
		// Already exists — update the projects path in case it changed
		existing, readErr := os.ReadFile(target)
		if readErr == nil {
			content := string(existing)
			// Only rewrite if it still contains a placeholder or we need to update the path
			if strings.Contains(content, "{{PROJECTS_PATH}}") {
				content = strings.ReplaceAll(content, "{{PROJECTS_PATH}}", projectsPath)
				return os.WriteFile(target, []byte(content), 0644)
			}
		}
		return nil
	}

	content := strings.ReplaceAll(welcomePageTmpl, "{{PROJECTS_PATH}}", projectsPath)
	return os.WriteFile(target, []byte(content), 0644)
}
