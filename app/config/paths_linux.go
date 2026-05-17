//go:build linux

package config

import (
	"os"
	"path/filepath"
)

func NewPlatformPaths() Paths {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".devour")
	return &basePaths{base: base}
}
