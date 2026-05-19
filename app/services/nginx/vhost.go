package nginx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

type SiteConfig struct {
	ServerName   string `json:"server_name"`
	DocumentRoot string `json:"document_root"`
	Listen       int    `json:"listen"`
	PHPFPMSocket string `json:"php_fpm_socket"`
	SSLEnabled   bool   `json:"ssl_enabled"`
	SSLCertPath  string `json:"ssl_cert_path"`
	SSLKeyPath   string `json:"ssl_key_path"`
	CustomConfig string `json:"custom_config"`
}

func (n *Nginx) CreateSite(site SiteConfig) error {
	sitesDir := n.GetSitesDir()
	if err := os.MkdirAll(sitesDir, 0755); err != nil {
		return fmt.Errorf("nginx: creating sites dir: %w", err)
	}

	fileName := strings.ReplaceAll(site.ServerName, ".", "_") + ".conf"
	confPath := filepath.Join(sitesDir, fileName)

	// site.conf.tmpl references PrefixDir (for fastcgi_params) and LogDir.
	// SiteConfig doesn't carry those — fold them in here so the rendered
	// vhost has valid paths.
	data := map[string]interface{}{
		"ServerName":   site.ServerName,
		"DocumentRoot": filepath.ToSlash(site.DocumentRoot),
		"Listen":       site.Listen,
		"PHPFPMSocket": site.PHPFPMSocket,
		"SSLEnabled":   site.SSLEnabled,
		"SSLCertPath":  filepath.ToSlash(site.SSLCertPath),
		"SSLKeyPath":   filepath.ToSlash(site.SSLKeyPath),
		"CustomConfig": site.CustomConfig,
		"PrefixDir":    filepath.ToSlash(n.GetPrefixDir()),
		"LogDir":       filepath.ToSlash(n.paths.LogsPath()),
	}

	return renderTemplateStr(siteConfTmpl, confPath, data)
}

func (n *Nginx) DeleteSite(serverName string) error {
	fileName := strings.ReplaceAll(serverName, ".", "_") + ".conf"
	confPath := filepath.Join(n.GetSitesDir(), fileName)
	if err := os.Remove(confPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("nginx: deleting site %s: %w", serverName, err)
	}
	return nil
}

func (n *Nginx) ListSites() ([]SiteConfig, error) {
	sitesDir := n.GetSitesDir()
	entries, err := os.ReadDir(sitesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nginx: listing sites: %w", err)
	}

	var sites []SiteConfig
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".conf") {
			name := strings.TrimSuffix(entry.Name(), ".conf")
			name = strings.ReplaceAll(name, "_", ".")
			sites = append(sites, SiteConfig{
				ServerName: name,
			})
		}
	}
	return sites, nil
}

func renderTemplateStr(tmplContent, outPath string, data interface{}) error {
	tmpl, err := template.New("tmpl").Parse(tmplContent)
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}

	outDir := filepath.Dir(outPath)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output file %s: %w", outPath, err)
	}
	defer f.Close()

	return tmpl.Execute(f, data)
}
