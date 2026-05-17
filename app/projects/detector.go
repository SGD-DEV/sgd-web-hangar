package projects

import (
	"os"
	"path/filepath"
)

type Detector struct{}

func NewDetector() *Detector {
	return &Detector{}
}

func (d *Detector) Detect(projectPath string) Framework {
	if d.fileExists(projectPath, "artisan") {
		return FrameworkLaravel
	}
	if d.fileExists(projectPath, "wp-config.php") || d.fileExists(projectPath, "wp-config-sample.php") {
		return FrameworkWordPress
	}
	if d.fileExists(projectPath, "symfony.lock") || d.fileExists(projectPath, "config", "bundles.php") {
		return FrameworkSymfony
	}
	if d.hasPhpFiles(projectPath) {
		return FrameworkPlainPHP
	}
	return FrameworkUnknown
}

func (d *Detector) GetDocumentRoot(projectPath string, framework Framework) string {
	switch framework {
	case FrameworkLaravel:
		publicDir := filepath.Join(projectPath, "public")
		if _, err := os.Stat(publicDir); err == nil {
			return publicDir
		}
	case FrameworkSymfony:
		publicDir := filepath.Join(projectPath, "public")
		if _, err := os.Stat(publicDir); err == nil {
			return publicDir
		}
	case FrameworkWordPress:
		return projectPath
	}
	return projectPath
}

func (d *Detector) fileExists(parts ...string) bool {
	_, err := os.Stat(filepath.Join(parts...))
	return err == nil
}

func (d *Detector) hasPhpFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".php" {
			return true
		}
	}
	return false
}
