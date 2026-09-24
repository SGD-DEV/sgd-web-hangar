package ssl

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/services"
)

func hideWindow(cmd *exec.Cmd) { services.HideWindow(cmd) }

type CertInfo struct {
	Domain    string `json:"domain"`
	CertPath  string `json:"cert_path"`
	KeyPath   string `json:"key_path"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

type Manager struct {
	paths config.Paths
	store *config.Store
}

func NewManager(paths config.Paths, store *config.Store) *Manager {
	return &Manager{
		paths: paths,
		store: store,
	}
}

// resolveMkcert returns a usable path to mkcert.exe, extracting the embedded
// copy on first use if the configured location doesn't have it. This is what
// makes SSL work on a fresh install — the NSIS installer doesn't ship
// assets/binaries/mkcert.exe to LOCALAPPDATA, so without this every SSL
// operation hit "mkcert not found".
func (m *Manager) resolveMkcert() (string, error) {
	dest := m.paths.MkcertPath()
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	path, err := ensureMkcert(dest)
	if err != nil {
		return "", fmt.Errorf("ssl: extracting embedded mkcert to %s: %w", dest, err)
	}
	return path, nil
}

// CARoot is where the local certificate authority (rootCA.pem + key) lives:
// inside Hangar's data folder, so it moves and gets backed up with the rest.
// A CA that mkcert created in its default location earlier is copied over
// once, so certificates already trusted by the browser stay valid.
func (m *Manager) CARoot() string {
	dir := filepath.Join(m.paths.SSLPath(), "ca")
	if _, err := os.Stat(filepath.Join(dir, "rootCA.pem")); err != nil {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			legacy := filepath.Join(local, "mkcert")
			if _, err := os.Stat(filepath.Join(legacy, "rootCA-key.pem")); err == nil {
				_ = os.MkdirAll(dir, 0700)
				for _, f := range []string{"rootCA.pem", "rootCA-key.pem"} {
					if data, err := os.ReadFile(filepath.Join(legacy, f)); err == nil {
						_ = os.WriteFile(filepath.Join(dir, f), data, 0600)
					}
				}
			}
		}
	}
	return dir
}

// mkcertCmd prepares an mkcert invocation that uses Hangar's CA folder. The
// window is NOT hidden: -install / -uninstall show Windows' "install this
// certificate?" confirmation, which a hidden process could never display.
func (m *Manager) mkcertCmd(mkcert string, args ...string) *exec.Cmd {
	cmd := exec.Command(mkcert, args...)
	cmd.Env = append(os.Environ(), "CAROOT="+m.CARoot())
	return cmd
}

// IsCAInstalled reports whether Windows trusts Hangar's root CA, i.e.
// browsers accept the local HTTPS certificates without a warning.
func (m *Manager) IsCAInstalled() bool {
	data, err := os.ReadFile(filepath.Join(m.CARoot(), "rootCA.pem"))
	if err != nil {
		return false
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	// With no Roots set, Verify asks the operating system's trust store.
	_, err = cert.Verify(x509.VerifyOptions{})
	return err == nil
}

func (m *Manager) InstallCA() error {
	mkcert, err := m.resolveMkcert()
	if err != nil {
		return err
	}

	cmd := m.mkcertCmd(mkcert, "-install")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssl: installing CA: %s: %w", string(output), err)
	}

	return nil
}

func (m *Manager) UninstallCA() error {
	mkcert, err := m.resolveMkcert()
	if err != nil {
		return err
	}

	cmd := m.mkcertCmd(mkcert, "-uninstall")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssl: uninstalling CA: %s: %w", string(output), err)
	}

	return nil
}

func (m *Manager) GenerateCert(domain string) error {
	mkcert, err := m.resolveMkcert()
	if err != nil {
		return err
	}

	sslDir := m.paths.SSLPath()
	if err := os.MkdirAll(sslDir, 0755); err != nil {
		return fmt.Errorf("ssl: creating ssl dir: %w", err)
	}

	certPath := filepath.Join(sslDir, domain+".pem")
	keyPath := filepath.Join(sslDir, domain+"-key.pem")

	cmd := m.mkcertCmd(mkcert,
		"-cert-file", certPath,
		"-key-file", keyPath,
		domain,
		"*."+domain,
	)
	hideWindow(cmd) // no console flash; generating needs no confirmation

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssl: generating cert for %s: %s: %w", domain, string(output), err)
	}

	certInfo := CertInfo{
		Domain:    domain,
		CertPath:  certPath,
		KeyPath:   keyPath,
		ExpiresAt: time.Now().AddDate(2, 0, 0).Format(time.RFC3339),
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	data, err := json.Marshal(certInfo)
	if err != nil {
		return fmt.Errorf("ssl: marshaling cert info: %w", err)
	}

	return m.store.SaveSSLCert(domain, data)
}

func (m *Manager) GetCert(domain string) (CertInfo, error) {
	data, err := m.store.GetSSLCert(domain)
	if err != nil {
		return CertInfo{}, fmt.Errorf("ssl: getting cert for %s: %w", domain, err)
	}

	var info CertInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return CertInfo{}, fmt.Errorf("ssl: unmarshaling cert for %s: %w", domain, err)
	}

	return info, nil
}

func (m *Manager) ListCerts() []CertInfo {
	allData, err := m.store.GetAllSSLCerts()
	if err != nil {
		return nil
	}

	certs := make([]CertInfo, 0, len(allData))
	for _, data := range allData {
		var info CertInfo
		if err := json.Unmarshal(data, &info); err == nil {
			certs = append(certs, info)
		}
	}

	return certs
}

func (m *Manager) DeleteCert(domain string) error {
	info, err := m.GetCert(domain)
	if err != nil {
		return err
	}

	os.Remove(info.CertPath)
	os.Remove(info.KeyPath)

	return m.store.SaveSSLCert(domain, nil)
}

func (m *Manager) RegenerateCert(domain string) error {
	m.DeleteCert(domain)
	return m.GenerateCert(domain)
}
