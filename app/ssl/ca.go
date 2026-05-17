package ssl

import (
	"fmt"
	"os"
	"os/exec"
)

func (m *Manager) IsCAInstalled() bool {
	mkcert := m.paths.MkcertPath()
	if _, err := os.Stat(mkcert); os.IsNotExist(err) {
		return false
	}

	cmd := exec.Command(mkcert, "-CAROOT")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	caRoot := string(output)
	if caRoot == "" {
		return false
	}

	return true
}

func (m *Manager) GetCARoot() (string, error) {
	mkcert := m.paths.MkcertPath()
	if _, err := os.Stat(mkcert); os.IsNotExist(err) {
		return "", fmt.Errorf("ssl: mkcert not found at %s", mkcert)
	}

	cmd := exec.Command(mkcert, "-CAROOT")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ssl: getting CA root: %w", err)
	}

	return string(output), nil
}
