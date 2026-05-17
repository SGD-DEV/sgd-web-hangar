package downloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type VersionEntry struct {
	Version  string `json:"version"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type VersionRegistry struct {
	Service  string         `json:"service"`
	Versions []VersionEntry `json:"versions"`
}

type Registry struct {
	registryDir string
}

func NewRegistry(registryDir string) *Registry {
	return &Registry{registryDir: registryDir}
}

func (r *Registry) Load(service string) (*VersionRegistry, error) {
	filePath := filepath.Join(r.registryDir, service+"-versions.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("registry: loading %s: %w", service, err)
	}

	var reg VersionRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("registry: parsing %s: %w", service, err)
	}

	return &reg, nil
}

func (r *Registry) GetVersion(service, version string) (*VersionEntry, error) {
	reg, err := r.Load(service)
	if err != nil {
		return nil, err
	}

	for _, v := range reg.Versions {
		if v.Version == version {
			return &v, nil
		}
	}

	return nil, fmt.Errorf("registry: version %s not found for %s", version, service)
}

func (r *Registry) ListVersions(service string) ([]VersionEntry, error) {
	reg, err := r.Load(service)
	if err != nil {
		return nil, err
	}
	return reg.Versions, nil
}
