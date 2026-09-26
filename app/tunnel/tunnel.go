// Package tunnel manages a Cloudflare Tunnel (cloudflared) for publishing
// Hangar's sites without opening ports on the router.
//
// Layout on disk (all under one folder, default <data>/cloudflared):
//
//	config.yml        tunnel id, credentials-file and the ingress rules
//	<tunnel-id>.json  tunnel credentials (written by `tunnel create`)
//	cert.pem          account certificate from `tunnel login` (only needed
//	                  to create tunnels and DNS routes, not to run one)
//	cloudflared.exe   stable copy the Windows service runs
//
// The tunnel itself runs as a Windows service (installed through NSSM), so
// it keeps serving even when Hangar is closed. Hangar edits the config and
// restarts that service.
package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/services"
	"gopkg.in/yaml.v3"
)

// IngressRule is one entry of the ingress list. Only the fields the panel
// edits are modelled; unknown per-rule keys are preserved on save.
type IngressRule struct {
	Hostname string `json:"hostname" yaml:"hostname,omitempty"`
	Path     string `json:"path" yaml:"path,omitempty"`
	Service  string `json:"service" yaml:"service"`
	// NoTLSVerify maps to originRequest.noTLSVerify, needed when the
	// origin is https with a self-signed (mkcert) certificate.
	NoTLSVerify bool `json:"no_tls_verify" yaml:"-"`
	// FromProject names the Hangar project a rule was generated for; it is
	// not stored in the YAML, the UI uses it for display.
	FromProject string `json:"from_project,omitempty" yaml:"-"`
}

// Config is the structured view of config.yml the Tunnel page edits.
type Config struct {
	Tunnel          string        `json:"tunnel"`
	CredentialsFile string        `json:"credentials_file"`
	Ingress         []IngressRule `json:"ingress"`
}

// Info is everything the Tunnel page shows in its header.
type Info struct {
	ConfigPath      string `json:"config_path"`
	ConfigExists    bool   `json:"config_exists"`
	Dir             string `json:"dir"`
	CloudflaredPath string `json:"cloudflared_path"`
	Version         string `json:"version"`
	LoggedIn        bool   `json:"logged_in"`
	ServiceName     string `json:"service_name"`
	ServiceState    string `json:"service_state"` // running | stopped | not-installed | unknown
	ServiceDetail   string `json:"service_detail,omitempty"`
	// Mode is "remote" when the service runs a dashboard tunnel from a
	// token (routes live at Cloudflare), "local" for config.yml tunnels.
	Mode     string `json:"mode"`
	TunnelID string `json:"tunnel_id,omitempty"`
	APIToken bool   `json:"api_token"`
}

// Tunnel is one tunnel from `cloudflared tunnel list`.
type Tunnel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at"`
	Connections int    `json:"connections"`
}

type Manager struct {
	paths config.Paths
	store *config.Store
}

func NewManager(paths config.Paths, store *config.Store) *Manager {
	return &Manager{paths: paths, store: store}
}

// ConfigPath returns the configured config.yml location.
func (m *Manager) ConfigPath() string {
	if cfg, err := m.store.GetAppConfig(); err == nil && cfg.TunnelConfigPath != "" {
		return cfg.TunnelConfigPath
	}
	return filepath.Join(m.paths.DataPath(), "cloudflared", "config.yml")
}

// Dir is the folder holding config, credentials and the service binary.
func (m *Manager) Dir() string { return filepath.Dir(m.ConfigPath()) }

func (m *Manager) ServiceName() string {
	if cfg, err := m.store.GetAppConfig(); err == nil && cfg.TunnelServiceName != "" {
		return cfg.TunnelServiceName
	}
	return "Cloudflared"
}

// certPath is where `cloudflared tunnel login` leaves the account cert.
func certPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cloudflared", "cert.pem")
}

// Cloudflared locates a cloudflared executable: the stable copy next to the
// config, then a Hangar-installed package, then PATH.
func (m *Manager) Cloudflared() string {
	stable := filepath.Join(m.Dir(), "cloudflared.exe")
	if _, err := os.Stat(stable); err == nil {
		return stable
	}
	base := filepath.Join(m.paths.InstalledPath(), "cloudflared")
	if entries, err := os.ReadDir(base); err == nil {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
		for _, e := range entries {
			for _, name := range []string{"cloudflared-windows-amd64.exe", "cloudflared.exe"} {
				p := filepath.Join(base, e.Name(), name)
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
	}
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p
	}
	return ""
}

func (m *Manager) run(timeout time.Duration, args ...string) (string, error) {
	exe := m.Cloudflared()
	if exe == "" {
		return "", fmt.Errorf("cloudflared is not installed - install it on the Packages page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = m.Dir()
	services.HideWindow(cmd)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("cloudflared timed out")
	}
	if err != nil {
		return out, fmt.Errorf("%s", firstNonEmpty(lastLines(out, 6), err.Error()))
	}
	return out, nil
}

// GetInfo gathers status for the page header.
func (m *Manager) GetInfo() Info {
	info := Info{
		ConfigPath:      m.ConfigPath(),
		Dir:             m.Dir(),
		CloudflaredPath: m.Cloudflared(),
		ServiceName:     m.ServiceName(),
	}
	if _, err := os.Stat(info.ConfigPath); err == nil {
		info.ConfigExists = true
	}
	if _, err := os.Stat(certPath()); err == nil {
		info.LoggedIn = true
	}
	if info.CloudflaredPath != "" {
		if out, err := m.run(10*time.Second, "--version"); err == nil {
			info.Version = strings.TrimPrefix(strings.SplitN(out, " (", 2)[0], "cloudflared version ")
		}
	}
	info.ServiceState, info.ServiceDetail = serviceState(info.ServiceName)
	info.Mode = "local"
	if ref := m.RemoteRef(); ref.TunnelID != "" {
		info.Mode = "remote"
		info.TunnelID = ref.TunnelID
		info.APIToken = m.HasAPIToken()
	}
	return info
}

// ReadRaw returns config.yml as text ("" if it doesn't exist yet).
func (m *Manager) ReadRaw() (string, error) {
	data, err := os.ReadFile(m.ConfigPath())
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

// Validate checks YAML syntax and asks cloudflared to validate the ingress
// rules (catch-all last, valid services, ...).
func (m *Manager) Validate(raw string) error {
	var probe map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &probe); err != nil {
		return fmt.Errorf("YAML: %w", err)
	}
	if _, ok := probe["ingress"]; !ok {
		return nil // nothing cloudflared could check
	}
	if m.Cloudflared() == "" {
		return nil
	}
	tmp, err := os.CreateTemp("", "hangar-tunnel-*.yml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.WriteString(tmp, raw); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	if _, err := m.run(20*time.Second, "tunnel", "--config", tmp.Name(), "ingress", "validate"); err != nil {
		return fmt.Errorf("cloudflared: %w", err)
	}
	return nil
}

// SaveRaw validates and writes config.yml, keeping the previous version as
// config.yml.bak.
func (m *Manager) SaveRaw(raw string) error {
	if err := m.Validate(raw); err != nil {
		return err
	}
	path := m.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", old, 0644)
	}
	return os.WriteFile(path, []byte(raw), 0644)
}

// GetConfig parses config.yml into the structured form.
func (m *Manager) GetConfig() (Config, error) {
	raw, err := m.ReadRaw()
	if err != nil {
		return Config{}, err
	}
	return parseConfig(raw)
}

type rawRule struct {
	Hostname      string                 `yaml:"hostname"`
	Path          string                 `yaml:"path"`
	Service       string                 `yaml:"service"`
	OriginRequest map[string]interface{} `yaml:"originRequest"`
}

func parseConfig(raw string) (Config, error) {
	var doc struct {
		Tunnel          string    `yaml:"tunnel"`
		CredentialsFile string    `yaml:"credentials-file"`
		Ingress         []rawRule `yaml:"ingress"`
	}
	if strings.TrimSpace(raw) != "" {
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			return Config{}, fmt.Errorf("YAML: %w", err)
		}
	}
	cfg := Config{Tunnel: doc.Tunnel, CredentialsFile: doc.CredentialsFile}
	for _, r := range doc.Ingress {
		rule := IngressRule{Hostname: r.Hostname, Path: r.Path, Service: r.Service}
		if v, ok := r.OriginRequest["noTLSVerify"].(bool); ok {
			rule.NoTLSVerify = v
		}
		cfg.Ingress = append(cfg.Ingress, rule)
	}
	return cfg, nil
}

var validHostname = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// SaveConfig writes the structured form back into config.yml. Top-level keys
// the panel doesn't know (and comments) are preserved; per-rule
// originRequest settings other than noTLSVerify are kept for rules whose
// hostname+path is unchanged. The catch-all 404 rule is always appended.
func (m *Manager) SaveConfig(c Config) error {
	raw, err := m.ReadRaw()
	if err != nil {
		return err
	}
	var root yaml.Node
	if strings.TrimSpace(raw) != "" {
		if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
			return fmt.Errorf("YAML: %w", err)
		}
	}
	if root.Kind == 0 {
		root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	top := root.Content[0]
	if top.Kind != yaml.MappingNode {
		return fmt.Errorf("config.yml: top level must be a mapping")
	}

	// Old originRequest blocks, keyed by hostname+path, to carry over.
	oldOrigin := map[string]map[string]interface{}{}
	if old, err := parseRawRules(raw); err == nil {
		for _, r := range old {
			if r.OriginRequest != nil {
				oldOrigin[r.Hostname+"|"+r.Path] = r.OriginRequest
			}
		}
	}

	var rules []map[string]interface{}
	for _, r := range c.Ingress {
		r.Hostname = strings.ToLower(strings.TrimSpace(r.Hostname))
		r.Service = strings.TrimSpace(r.Service)
		if r.Hostname == "" && r.Path == "" {
			continue // catch-all is appended below
		}
		if r.Hostname != "" && !validHostname.MatchString(r.Hostname) {
			return fmt.Errorf("invalid hostname %q", r.Hostname)
		}
		if r.Service == "" {
			return fmt.Errorf("rule %s: service is required (e.g. http://127.0.0.1:80)", r.Hostname)
		}
		rule := map[string]interface{}{}
		if r.Hostname != "" {
			rule["hostname"] = r.Hostname
		}
		if r.Path != "" {
			rule["path"] = r.Path
		}
		rule["service"] = r.Service
		origin := map[string]interface{}{}
		for k, v := range oldOrigin[r.Hostname+"|"+r.Path] {
			origin[k] = v
		}
		if r.NoTLSVerify {
			origin["noTLSVerify"] = true
		} else {
			delete(origin, "noTLSVerify")
		}
		if len(origin) > 0 {
			rule["originRequest"] = origin
		}
		rules = append(rules, rule)
	}
	rules = append(rules, map[string]interface{}{"service": "http_status:404"})

	var ingressNode yaml.Node
	if err := ingressNode.Encode(rules); err != nil {
		return err
	}
	setKey(top, "tunnel", c.Tunnel)
	setKey(top, "credentials-file", c.CredentialsFile)
	setNode(top, "ingress", &ingressNode)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return err
	}
	return m.SaveRaw(buf.String())
}

func parseRawRules(raw string) ([]rawRule, error) {
	var doc struct {
		Ingress []rawRule `yaml:"ingress"`
	}
	err := yaml.Unmarshal([]byte(raw), &doc)
	return doc.Ingress, err
}

func setKey(mapping *yaml.Node, key, value string) {
	if value == "" {
		return
	}
	setNode(mapping, key, &yaml.Node{Kind: yaml.ScalarNode, Value: value})
}

func setNode(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			// Keep comments attached to the old value.
			value.HeadComment = mapping.Content[i+1].HeadComment
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
}

// --- Account / tunnel lifecycle (wrappers around the cloudflared CLI) ---

// Login runs `cloudflared tunnel login`, which opens the browser for the
// Cloudflare account + zone selection and writes cert.pem. Blocks until the
// user finished in the browser (or 5 minutes pass).
func (m *Manager) Login() error {
	if _, err := os.Stat(certPath()); err == nil {
		return nil
	}
	_, err := m.run(5*time.Minute, "tunnel", "login")
	return err
}

// ListTunnels returns the account's tunnels (needs Login).
func (m *Manager) ListTunnels() ([]Tunnel, error) {
	out, err := m.run(30*time.Second, "tunnel", "list", "--output", "json")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID          string            `json:"id"`
		Name        string            `json:"name"`
		CreatedAt   string            `json:"created_at"`
		Connections []json.RawMessage `json:"connections"`
	}
	if err := json.Unmarshal([]byte(jsonPart(out)), &raw); err != nil {
		return nil, fmt.Errorf("parsing tunnel list: %w", err)
	}
	var res []Tunnel
	for _, t := range raw {
		res = append(res, Tunnel{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt, Connections: len(t.Connections)})
	}
	return res, nil
}

var validTunnelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`)

// CreateTunnel creates a named tunnel, stores its credentials next to the
// config and points config.yml at it. Existing ingress rules are kept.
func (m *Manager) CreateTunnel(name string) (Tunnel, error) {
	if !validTunnelName.MatchString(name) {
		return Tunnel{}, fmt.Errorf("tunnel name may contain letters, digits, - and _")
	}
	if err := os.MkdirAll(m.Dir(), 0755); err != nil {
		return Tunnel{}, err
	}
	out, err := m.run(60*time.Second, "tunnel", "create", "--output", "json", "--credentials-file", filepath.Join(m.Dir(), name+".json"), name)
	if err != nil {
		return Tunnel{}, err
	}
	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(jsonPart(out)), &created); err != nil || created.ID == "" {
		return Tunnel{}, fmt.Errorf("unexpected output from cloudflared: %s", lastLines(out, 4))
	}
	// Name the credentials after the id, as cloudflared does by default.
	credsName := filepath.Join(m.Dir(), name+".json")
	credsID := filepath.Join(m.Dir(), created.ID+".json")
	if err := os.Rename(credsName, credsID); err != nil {
		credsID = credsName
	}
	return Tunnel{ID: created.ID, Name: created.Name}, m.UseTunnel(created.ID, credsID)
}

// UseTunnel points config.yml at an existing tunnel id + credentials file.
func (m *Manager) UseTunnel(id, credentialsFile string) error {
	cfg, err := m.GetConfig()
	if err != nil {
		return err
	}
	cfg.Tunnel = id
	if credentialsFile != "" {
		cfg.CredentialsFile = credentialsFile
	}
	return m.SaveConfig(cfg)
}

// RouteDNS creates (or overwrites) the CNAME for hostname pointing at the
// configured tunnel.
func (m *Manager) RouteDNS(hostname string) (string, error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if !validHostname.MatchString(hostname) {
		return "", fmt.Errorf("invalid hostname %q", hostname)
	}
	cfg, err := m.GetConfig()
	if err != nil {
		return "", err
	}
	if cfg.Tunnel == "" {
		return "", fmt.Errorf("no tunnel configured yet")
	}
	out, err := m.run(60*time.Second, "tunnel", "route", "dns", "--overwrite-dns", cfg.Tunnel, hostname)
	return lastLines(out, 2), err
}

// --- helpers ---

func jsonPart(s string) string {
	// cloudflared may print log lines before the JSON document.
	if i := strings.IndexAny(s, "[{"); i >= 0 {
		return s[i:]
	}
	return s
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
