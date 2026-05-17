package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths interface {
	BasePath() string
	DataPath() string
	InstalledPath() string
	SSLPath() string
	LogsPath() string
	DBPath() string
	WwwPath() string
	ProjectsPath() string
	PHPPath(version string) string
	ApachePath(version string) string
	NginxPath(version string) string
	MySQLPath(version string) string
	PostgreSQLPath(version string) string
	ConfPath(service string) string
	MkcertPath() string
	RegistryPath() string
	EnsureDirectories() error
}

type basePaths struct {
	base string
}

func (p *basePaths) BasePath() string {
	return p.base
}

func (p *basePaths) DataPath() string {
	return filepath.Join(p.base, "data")
}

func (p *basePaths) InstalledPath() string {
	return filepath.Join(p.base, "data", "installed")
}

func (p *basePaths) WwwPath() string {
	return filepath.Join(p.base, "data", "www")
}

func (p *basePaths) ProjectsPath() string {
	return filepath.Join(p.base, "projects")
}

func (p *basePaths) SSLPath() string {
	return filepath.Join(p.base, "data", "ssl")
}

func (p *basePaths) LogsPath() string {
	return filepath.Join(p.base, "data", "logs")
}

func (p *basePaths) DBPath() string {
	return filepath.Join(p.base, "data", "devour.db")
}

func (p *basePaths) PHPPath(version string) string {
	return filepath.Join(p.InstalledPath(), "php", version)
}

func (p *basePaths) ApachePath(version string) string {
	return filepath.Join(p.InstalledPath(), "apache", version)
}

func (p *basePaths) NginxPath(version string) string {
	return filepath.Join(p.InstalledPath(), "nginx", version)
}

func (p *basePaths) MySQLPath(version string) string {
	return filepath.Join(p.InstalledPath(), "mysql", version)
}

func (p *basePaths) PostgreSQLPath(version string) string {
	return filepath.Join(p.InstalledPath(), "postgresql", version)
}

func (p *basePaths) ConfPath(service string) string {
	return filepath.Join(p.DataPath(), "conf", service)
}

func (p *basePaths) MkcertPath() string {
	return filepath.Join(p.base, "assets", "binaries", "mkcert.exe")
}

func (p *basePaths) RegistryPath() string {
	return filepath.Join(p.base, "assets", "registry")
}

func (p *basePaths) EnsureDirectories() error {
	dirs := []string{
		p.DataPath(),
		p.InstalledPath(),
		filepath.Join(p.InstalledPath(), "php"),
		filepath.Join(p.InstalledPath(), "apache"),
		filepath.Join(p.InstalledPath(), "nginx"),
		filepath.Join(p.InstalledPath(), "mysql"),
		filepath.Join(p.InstalledPath(), "postgresql"),
		filepath.Join(p.InstalledPath(), "node"),
		filepath.Join(p.InstalledPath(), "go"),
		filepath.Join(p.InstalledPath(), "phpmyadmin"),
		filepath.Join(p.InstalledPath(), "dbeaver"),
		filepath.Join(p.InstalledPath(), "vscode"),
		filepath.Join(p.InstalledPath(), "heidisql"),
		filepath.Join(p.InstalledPath(), "pocketbase"),
		p.WwwPath(),
		p.ProjectsPath(),
		p.SSLPath(),
		p.LogsPath(),
		p.ConfPath("apache"),
		p.ConfPath("nginx"),
		p.ConfPath("mysql"),
		p.ConfPath("postgresql"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("config: creating directory %s: %w", dir, err)
		}
	}
	return nil
}
