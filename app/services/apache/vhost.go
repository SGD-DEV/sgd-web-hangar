package apache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

type VirtualHost struct {
	ServerName   string `json:"server_name"`
	DocumentRoot string `json:"document_root"`
	Port         int    `json:"port"`
	PHPVersion   string `json:"php_version"`
	SSLEnabled   bool   `json:"ssl_enabled"`
	SSLCertPath  string `json:"ssl_cert_path"`
	SSLKeyPath   string `json:"ssl_key_path"`
	CustomConfig string `json:"custom_config"`
}

func (a *Apache) CreateVhost(vhost VirtualHost) error {
	vhostDir := a.GetVhostDir()
	if err := os.MkdirAll(vhostDir, 0755); err != nil {
		return fmt.Errorf("apache: creating vhost dir: %w", err)
	}

	fileName := strings.ReplaceAll(vhost.ServerName, ".", "_") + ".conf"
	confPath := filepath.Join(vhostDir, fileName)

	// vhost.conf.tmpl references {{.LogDir}} which VirtualHost doesn't
	// carry — fold it in here so the rendered ErrorLog/CustomLog paths
	// aren't empty strings.
	data := map[string]interface{}{
		"ServerName":   vhost.ServerName,
		"DocumentRoot": filepath.ToSlash(vhost.DocumentRoot),
		"Port":         vhost.Port,
		"PHPVersion":   vhost.PHPVersion,
		"SSLEnabled":   vhost.SSLEnabled,
		"SSLCertPath":  filepath.ToSlash(vhost.SSLCertPath),
		"SSLKeyPath":   filepath.ToSlash(vhost.SSLKeyPath),
		"CustomConfig": vhost.CustomConfig,
		"LogDir":       filepath.ToSlash(a.paths.LogsPath()),
	}

	return renderTemplateStr(vhostConfTmpl, confPath, data)
}

func (a *Apache) DeleteVhost(serverName string) error {
	fileName := strings.ReplaceAll(serverName, ".", "_") + ".conf"
	confPath := filepath.Join(a.GetVhostDir(), fileName)
	if err := os.Remove(confPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("apache: deleting vhost %s: %w", serverName, err)
	}
	return nil
}

func (a *Apache) ListVhosts() ([]VirtualHost, error) {
	vhostDir := a.GetVhostDir()
	entries, err := os.ReadDir(vhostDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("apache: listing vhosts: %w", err)
	}

	var vhosts []VirtualHost
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".conf") {
			name := strings.TrimSuffix(entry.Name(), ".conf")
			name = strings.ReplaceAll(name, "_", ".")
			vhosts = append(vhosts, VirtualHost{
				ServerName: name,
			})
		}
	}
	return vhosts, nil
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

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	return nil
}
