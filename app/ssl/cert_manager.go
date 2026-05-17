package ssl

import (
	"os"
	"time"
)

func (m *Manager) IsCertValid(domain string) bool {
	info, err := m.GetCert(domain)
	if err != nil {
		return false
	}

	if _, err := os.Stat(info.CertPath); os.IsNotExist(err) {
		return false
	}
	if _, err := os.Stat(info.KeyPath); os.IsNotExist(err) {
		return false
	}

	if info.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339, info.ExpiresAt)
		if err == nil && time.Now().After(expiry) {
			return false
		}
	}

	return true
}

func (m *Manager) GetCertPaths(domain string) (certPath, keyPath string, err error) {
	info, err := m.GetCert(domain)
	if err != nil {
		return "", "", err
	}
	return info.CertPath, info.KeyPath, nil
}

func (m *Manager) EnsureCert(domain string) error {
	if m.IsCertValid(domain) {
		return nil
	}
	return m.GenerateCert(domain)
}
