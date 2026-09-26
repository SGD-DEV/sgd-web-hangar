package app

// app_tunnel.go exposes the Cloudflare Tunnel manager (app/tunnel) to the
// frontend's Tunnel page.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devour-app/devour/app/tunnel"
)

func (a *App) tunnelMgr() (*tunnel.Manager, error) {
	if a.tunnel == nil {
		return nil, fmt.Errorf("tunnel manager not initialized")
	}
	return a.tunnel, nil
}

func (a *App) GetTunnelInfo() (tunnel.Info, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return tunnel.Info{}, err
	}
	return t.GetInfo(), nil
}

// GetTunnelConfig returns the parsed config with each rule annotated with the
// project that answers to its hostname.
func (a *App) GetTunnelConfig() (tunnel.Config, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return tunnel.Config{}, err
	}
	cfg, err := t.GetConfig()
	if err != nil {
		return cfg, err
	}
	owners := a.hostnameOwners()
	for i := range cfg.Ingress {
		cfg.Ingress[i].FromProject = owners[cfg.Ingress[i].Hostname]
	}
	return cfg, nil
}

func (a *App) SaveTunnelConfig(cfg tunnel.Config) error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	return t.SaveConfig(cfg)
}

func (a *App) GetTunnelConfigRaw() (string, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return "", err
	}
	return t.ReadRaw()
}

func (a *App) ValidateTunnelConfig(raw string) error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	return t.Validate(raw)
}

func (a *App) SaveTunnelConfigRaw(raw string) error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	return t.SaveRaw(raw)
}

// WebServerOrigin is the URL cloudflared forwards project traffic to: the
// active web server on localhost.
func (a *App) WebServerOrigin() string {
	port := 80
	if cfg, err := a.config.GetAppConfig(); err == nil {
		switch cfg.ActiveWebServer {
		case "nginx":
			if cfg.NginxPort > 0 {
				port = cfg.NginxPort
			}
		default:
			if cfg.ApachePort > 0 {
				port = cfg.ApachePort
			}
		}
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func (a *App) hostnameOwners() map[string]string {
	owners := map[string]string{}
	if a.projectManager == nil {
		return owners
	}
	for _, p := range a.projectManager.List() {
		for _, alias := range p.Aliases {
			owners[alias] = p.Name
		}
	}
	return owners
}

// SyncTunnelFromProjects makes the ingress list match the projects' public
// domains (see mergeProjectRules). Returns the new config without saving,
// so the user can review it first.
func (a *App) SyncTunnelFromProjects() (tunnel.Config, error) {
	cfg, err := a.GetTunnelConfig()
	if err != nil {
		return cfg, err
	}
	cfg.Ingress = a.mergeProjectRules(cfg.Ingress)
	return cfg, nil
}

// mergeProjectRules gives every project alias a rule to the web server and
// drops web-server rules whose hostname no project claims any more. Rules
// for other services (Filebrowser, an app on another port, ...) are left
// alone.
func (a *App) mergeProjectRules(existing []tunnel.IngressRule) []tunnel.IngressRule {
	origin := a.WebServerOrigin()
	isWebOrigin := func(service string) bool {
		s := strings.TrimRight(strings.ToLower(service), "/")
		port := origin[strings.LastIndex(origin, ":"):]
		return s == origin || s == "http://localhost"+port || (port == ":80" && (s == "http://127.0.0.1" || s == "http://localhost"))
	}
	owners := a.hostnameOwners()

	rules := []tunnel.IngressRule{}
	have := map[string]bool{}
	for _, r := range existing {
		if r.Hostname == "" && r.Path == "" {
			continue // catch-all, re-added on save
		}
		if isWebOrigin(r.Service) && owners[r.Hostname] == "" && r.Path == "" {
			continue // stale: no project answers to this host any more
		}
		have[r.Hostname] = true
		r.FromProject = owners[r.Hostname]
		rules = append(rules, r)
	}
	for _, p := range a.projectManager.List() {
		for _, alias := range p.Aliases {
			if have[alias] {
				continue
			}
			rules = append(rules, tunnel.IngressRule{Hostname: alias, Service: origin, FromProject: p.Name})
			have[alias] = true
		}
	}
	return rules
}

// --- Dashboard (token) tunnels, edited through the Cloudflare API ---

// SetCloudflareAPIToken checks and stores the API token, then publishes
// the projects' domains right away.
func (a *App) SetCloudflareAPIToken(token string) (tunnel.SyncReport, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return tunnel.SyncReport{}, err
	}
	if err := t.SetAPIToken(token); err != nil {
		return tunnel.SyncReport{}, err
	}
	return a.SyncTunnelRemote()
}

// GetTunnelRemoteState returns routes, DNS status and leftovers of old
// tunnels; routes are annotated with the project that owns the hostname.
func (a *App) GetTunnelRemoteState() (tunnel.RemoteState, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return tunnel.RemoteState{}, err
	}
	st, err := t.GetRemoteState()
	owners := a.hostnameOwners()
	for i := range st.Routes {
		st.Routes[i].FromProject = owners[st.Routes[i].Hostname]
	}
	return st, err
}

// SyncTunnelRemote publishes every project's public domains: routes to the
// web server plus DNS records, and removes those of dropped domains.
func (a *App) SyncTunnelRemote() (tunnel.SyncReport, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return tunnel.SyncReport{}, err
	}
	rules, err := t.RemoteRoutes()
	if err != nil {
		return tunnel.SyncReport{}, err
	}
	return t.ApplyRemoteRoutes(a.mergeProjectRules(rules))
}

// SaveTunnelRemoteRoutes stores hand-made routes (Filebrowser, apps on
// other ports, ...); project domains are always kept.
func (a *App) SaveTunnelRemoteRoutes(rules []tunnel.IngressRule) (tunnel.SyncReport, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return tunnel.SyncReport{}, err
	}
	return t.ApplyRemoteRoutes(a.mergeProjectRules(rules))
}

func (a *App) DeleteTunnelDNSRecord(zoneID, recordID string) error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	return t.DeleteDNSRecord(zoneID, recordID)
}

func (a *App) DeleteRemoteTunnel(id string) error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	return t.DeleteRemoteTunnel(id)
}

// TunnelAutoPublish reports whether saving a project publishes its public
// domains automatically.
func (a *App) TunnelAutoPublish() bool {
	return a.tunnel != nil && a.tunnel.RemoteReady()
}

// autoPublishTunnel runs SyncTunnelRemote after a project's domains changed
// when Hangar manages a dashboard tunnel; otherwise it does nothing.
func (a *App) autoPublishTunnel() error {
	if !a.TunnelAutoPublish() {
		return nil
	}
	rep, err := a.SyncTunnelRemote()
	if err != nil {
		return err
	}
	if len(rep.Errors) > 0 {
		return fmt.Errorf("%s", strings.Join(rep.Errors, "; "))
	}
	return nil
}

func (a *App) TunnelLogin() error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	return t.Login()
}

func (a *App) ListTunnels() ([]tunnel.Tunnel, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return nil, err
	}
	return t.ListTunnels()
}

func (a *App) CreateTunnel(name string) (tunnel.Tunnel, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return tunnel.Tunnel{}, err
	}
	return t.CreateTunnel(strings.TrimSpace(name))
}

// UseExistingTunnel points the config at a tunnel created elsewhere. Its
// credentials file must already be in the tunnel folder or in
// %USERPROFILE%\.cloudflared (then it is copied over).
func (a *App) UseExistingTunnel(id string) error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	creds := filepath.Join(t.Dir(), id+".json")
	if _, err := os.Stat(creds); err != nil {
		home, _ := os.UserHomeDir()
		src := filepath.Join(home, ".cloudflared", id+".json")
		data, readErr := os.ReadFile(src)
		if readErr != nil {
			return fmt.Errorf("credentials for tunnel %s not found (expected %s)", id, src)
		}
		if err := os.MkdirAll(t.Dir(), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(creds, data, 0600); err != nil {
			return err
		}
	}
	return t.UseTunnel(id, creds)
}

func (a *App) RouteTunnelDNS(hostname string) (string, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return "", err
	}
	return t.RouteDNS(hostname)
}

func (a *App) RestartTunnel() error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	return t.RestartService()
}

func (a *App) StopTunnel() error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	return t.StopService()
}

func (a *App) InstallTunnelService() error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	return t.InstallService()
}

func (a *App) UninstallTunnelService() error {
	t, err := a.tunnelMgr()
	if err != nil {
		return err
	}
	return t.UninstallService()
}

// GetTunnelLog returns the tail of the tunnel service's log file.
func (a *App) GetTunnelLog(lines int) ([]string, error) {
	t, err := a.tunnelMgr()
	if err != nil {
		return nil, err
	}
	return tailFile(filepath.Join(t.Dir(), "service.log"), lines)
}

// tailFile returns the last n lines of a text file (reads at most 256 KB).
func tailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const maxRead = 256 * 1024
	size := info.Size()
	offset := int64(0)
	if size > maxRead {
		offset = size - maxRead
	}
	buf := make([]byte, size-offset)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimRight(string(buf), "\r\n"), "\r\n", "\n"), "\n")
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
