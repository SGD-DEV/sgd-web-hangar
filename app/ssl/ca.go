package ssl

import (
	"fmt"
	"os/exec"
)

func (m *Manager) IsCAInstalled() bool {
	mkcert, err := m.resolveMkcert()
	if err != nil {
		return false
	}

	cmd := exec.Command(mkcert, "-CAROOT")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	if string(output) == "" {
		return false
	}

	return true
}

func (m *Manager) GetCARoot() (string, error) {
	mkcert, err := m.resolveMkcert()
	if err != nil {
		return "", err
	}

	cmd := exec.Command(mkcert, "-CAROOT")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ssl: getting CA root: %w", err)
	}

	return string(output), nil
}
