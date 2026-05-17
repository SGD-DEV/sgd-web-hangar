//go:build darwin

package config

import (
	"os"
	"path/filepath"
)

func NewPlatformPaths() Paths {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, "Library", "Application Support", "Devour")
	return &basePaths{base: base}
}
