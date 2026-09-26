package tunnel

// remote.go handles tunnels created in the Cloudflare dashboard. Their
// service runs `cloudflared tunnel run --token-file <file>` (or --token) and
// the routes live at Cloudflare, not in config.yml. With an API token
// (permissions: Account > Cloudflare Tunnel > Edit, Zone > DNS > Edit)
// Hangar edits those routes and the matching DNS records itself, so a new
// public domain on a project goes live without opening the dashboard.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

const apiBase = "https://api.cloudflare.com/client/v4"

// RemoteRef identifies the dashboard-managed tunnel the service runs.
type RemoteRef struct {
	AccountID string `json:"account_id"`
	TunnelID  string `json:"tunnel_id"`
	// TokenFile is the --token-file the service reads ("" for --token).
	TokenFile string `json:"token_file"`
}

// RemoteTunnel is a tunnel as the Cloudflare API reports it.
type RemoteTunnel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"` // healthy | degraded | down | inactive
	Connections int    `json:"connections"`
	CreatedAt   string `json:"created_at"`
}

// DNSRecord is a DNS record in one of the account's zones.
type DNSRecord struct {
	ZoneID   string `json:"zone_id"`
	ZoneName string `json:"zone_name"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
}

// RemoteState is what the Tunnel page shows in token mode.
type RemoteState struct {
	Tunnel RemoteTunnel   `json:"tunnel"`
	Routes []IngressRule  `json:"routes"`
	Target string         `json:"target"` // <id>.cfargotunnel.com
	// MissingDNS are route hostnames without a DNS record pointing here.
	MissingDNS []string `json:"missing_dns"`
	// ForeignDNS are records that point at other tunnels (old servers).
	ForeignDNS []DNSRecord `json:"foreign_dns"`
	// OtherTunnels are the account's other tunnels.
	OtherTunnels []RemoteTunnel `json:"other_tunnels"`
}

// SyncReport lists what an update changed at Cloudflare.
type SyncReport struct {
	Changes []string `json:"changes"`
	Errors  []string `json:"errors"`
}

var remoteMu sync.Mutex

// RemoteRef reads the tunnel token the service was installed with. The
// zero value means the service is not a token (dashboard) tunnel.
func (m *Manager) RemoteRef() RemoteRef {
	args := splitArgs(serviceParameters(m.ServiceName()))
	var token, file string
	for i, a := range args {
		switch {
		case a == "--token-file" && i+1 < len(args):
			file = args[i+1]
		case strings.HasPrefix(a, "--token-file="):
			file = strings.TrimPrefix(a, "--token-file=")
		case a == "--token" && i+1 < len(args):
			token = args[i+1]
		case strings.HasPrefix(a, "--token="):
			token = strings.TrimPrefix(a, "--token=")
		}
	}
	if token == "" && file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return RemoteRef{TokenFile: file}
		}
		token = strings.TrimSpace(string(data))
	}
	if token == "" {
		return RemoteRef{}
	}
	var t struct {
		A string `json:"a"`
		T string `json:"t"`
	}
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(token, "="))
	}
	if err != nil || json.Unmarshal(raw, &t) != nil {
		return RemoteRef{TokenFile: file}
	}
	return RemoteRef{AccountID: t.A, TunnelID: t.T, TokenFile: file}
}

// splitArgs splits a command line at spaces, honouring double quotes.
func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inQuote, have := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			have = true
		case (r == ' ' || r == '\t') && !inQuote:
			if have {
				args = append(args, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if have {
		args = append(args, cur.String())
	}
	return args
}

// APITokenPath is where the Cloudflare API token is kept: next to the
// tunnel token (the secrets folder) when there is one.
func (m *Manager) APITokenPath() string {
	dir := m.Dir()
	if f := m.RemoteRef().TokenFile; f != "" {
		dir = filepath.Dir(f)
	}
	return filepath.Join(dir, "cloudflare-api-token.txt")
}

func (m *Manager) HasAPIToken() bool {
	data, err := os.ReadFile(m.APITokenPath())
	return err == nil && strings.TrimSpace(string(data)) != ""
}

// SetAPIToken checks that the token can read the tunnel and stores it.
func (m *Manager) SetAPIToken(token string) error {
	token = strings.TrimSpace(token)
	ref := m.RemoteRef()
	if ref.TunnelID == "" {
		return fmt.Errorf("der Tunnel-Dienst läuft nicht mit einem Dashboard-Token")
	}
	if token == "" {
		return fmt.Errorf("kein Token eingegeben")
	}
	c := &cfClient{token: token}
	if _, err := c.tunnel(ref); err != nil {
		return fmt.Errorf("Token funktioniert nicht: %w", err)
	}
	zones, err := c.zones()
	if err != nil {
		return fmt.Errorf("Token darf die Domains nicht lesen (Berechtigung Zone > DNS > Bearbeiten fehlt?): %w", err)
	}
	if len(zones) == 0 {
		return fmt.Errorf("der Token sieht keine Domain - bei den Zonenressourcen \"Alle Zonen\" wählen")
	}
	return os.WriteFile(m.APITokenPath(), []byte(token), 0600)
}

func (m *Manager) client() (*cfClient, RemoteRef, error) {
	ref := m.RemoteRef()
	if ref.TunnelID == "" {
		return nil, ref, fmt.Errorf("der Tunnel-Dienst läuft nicht mit einem Dashboard-Token")
	}
	data, err := os.ReadFile(m.APITokenPath())
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return nil, ref, fmt.Errorf("kein Cloudflare-API-Token hinterlegt (Seite Tunnel)")
	}
	return &cfClient{token: strings.TrimSpace(string(data))}, ref, nil
}

// RemoteReady reports whether Hangar can edit the dashboard tunnel.
func (m *Manager) RemoteReady() bool {
	return m.RemoteRef().TunnelID != "" && m.HasAPIToken()
}

// RemoteRoutes returns the tunnel's current ingress rules (without the
// catch-all).
func (m *Manager) RemoteRoutes() ([]IngressRule, error) {
	c, ref, err := m.client()
	if err != nil {
		return nil, err
	}
	cfg, err := c.tunnelConfig(ref)
	if err != nil {
		return nil, err
	}
	return rulesFromRemote(cfg), nil
}

// GetRemoteState gathers everything the Tunnel page shows in token mode.
func (m *Manager) GetRemoteState() (RemoteState, error) {
	c, ref, err := m.client()
	if err != nil {
		return RemoteState{}, err
	}
	st := RemoteState{Target: ref.TunnelID + ".cfargotunnel.com"}
	if st.Tunnel, err = c.tunnel(ref); err != nil {
		return st, err
	}
	cfg, err := c.tunnelConfig(ref)
	if err != nil {
		return st, err
	}
	st.Routes = rulesFromRemote(cfg)

	zones, err := c.zones()
	if err != nil {
		return st, err
	}
	pointsHere := map[string]bool{}
	for _, z := range zones {
		recs, err := c.dnsRecords(z, url.Values{"type": {"CNAME"}})
		if err != nil {
			return st, err
		}
		for _, r := range recs {
			content := strings.ToLower(r.Content)
			if !strings.HasSuffix(content, ".cfargotunnel.com") {
				continue
			}
			if content == st.Target {
				pointsHere[strings.ToLower(r.Name)] = true
			} else {
				st.ForeignDNS = append(st.ForeignDNS, r)
			}
		}
	}
	for _, r := range st.Routes {
		if r.Hostname != "" && !pointsHere[r.Hostname] {
			st.MissingDNS = append(st.MissingDNS, r.Hostname)
		}
	}
	all, err := c.tunnels(ref)
	if err != nil {
		return st, err
	}
	for _, t := range all {
		if t.ID != ref.TunnelID {
			st.OtherTunnels = append(st.OtherTunnels, t)
		}
	}
	return st, nil
}

// ApplyRemoteRoutes makes the tunnel's routes equal to rules (a catch-all
// 404 is appended) and fixes DNS: every route hostname gets a proxied CNAME
// to this tunnel - A/AAAA/CNAME records in the way are replaced - and our
// CNAMEs of hostnames that lost their route are deleted.
func (m *Manager) ApplyRemoteRoutes(rules []IngressRule) (SyncReport, error) {
	remoteMu.Lock()
	defer remoteMu.Unlock()
	rep := SyncReport{Changes: []string{}, Errors: []string{}}
	c, ref, err := m.client()
	if err != nil {
		return rep, err
	}
	cfg, err := c.tunnelConfig(ref)
	if err != nil {
		return rep, err
	}
	old := rulesFromRemote(cfg)

	// originRequest settings of unchanged rules are kept.
	oldOrigin := map[string]map[string]interface{}{}
	for _, r := range remoteIngress(cfg) {
		if o, ok := r["originRequest"].(map[string]interface{}); ok {
			oldOrigin[str(r["hostname"])+"|"+str(r["path"])] = o
		}
	}
	var ingress []interface{}
	seen := map[string]bool{}
	for _, r := range rules {
		r.Hostname = strings.ToLower(strings.TrimSpace(r.Hostname))
		r.Service = strings.TrimSpace(r.Service)
		if r.Hostname == "" {
			continue
		}
		if !validHostname.MatchString(r.Hostname) {
			return rep, fmt.Errorf("ungültiger Hostname %q", r.Hostname)
		}
		if r.Service == "" {
			return rep, fmt.Errorf("%s: Ziel fehlt (z. B. http://127.0.0.1:80)", r.Hostname)
		}
		key := r.Hostname + "|" + r.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		rule := map[string]interface{}{"hostname": r.Hostname, "service": r.Service}
		if r.Path != "" {
			rule["path"] = r.Path
		}
		origin := map[string]interface{}{}
		for k, v := range oldOrigin[key] {
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
		ingress = append(ingress, rule)
	}
	ingress = append(ingress, map[string]interface{}{"service": "http_status:404"})

	if !reflect.DeepEqual(normalizeJSON(ingress), normalizeJSON(cfg["ingress"])) {
		cfg["ingress"] = ingress
		if err := c.putTunnelConfig(ref, cfg); err != nil {
			return rep, err
		}
		oldSet := map[string]string{}
		for _, r := range old {
			oldSet[r.Hostname+r.Path] = r.Service
		}
		newSet := map[string]bool{}
		for _, r := range rulesFromRemote(cfg) {
			newSet[r.Hostname+r.Path] = true
			if svc, ok := oldSet[r.Hostname+r.Path]; !ok {
				rep.Changes = append(rep.Changes, fmt.Sprintf("Route %s%s → %s angelegt", r.Hostname, r.Path, r.Service))
			} else if svc != r.Service {
				rep.Changes = append(rep.Changes, fmt.Sprintf("Route %s%s → %s geändert", r.Hostname, r.Path, r.Service))
			}
		}
		for _, r := range old {
			if !newSet[r.Hostname+r.Path] {
				rep.Changes = append(rep.Changes, fmt.Sprintf("Route %s%s entfernt", r.Hostname, r.Path))
			}
		}
	}

	// DNS
	target := ref.TunnelID + ".cfargotunnel.com"
	zones, err := c.zones()
	if err != nil {
		return rep, err
	}
	want := map[string]bool{}
	for _, r := range rulesFromRemote(cfg) {
		want[r.Hostname] = true
	}
	for _, r := range old {
		if !want[r.Hostname] {
			if err := c.removeDNS(zones, r.Hostname, target, &rep); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("DNS %s: %v", r.Hostname, err))
			}
		}
	}
	hosts := make([]string, 0, len(want))
	for h := range want {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	for _, h := range hosts {
		if err := c.ensureDNS(zones, h, target, &rep); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("DNS %s: %v", h, err))
		}
	}
	return rep, nil
}

// DeleteDNSRecord removes one record (used for leftovers of old tunnels).
func (m *Manager) DeleteDNSRecord(zoneID, recordID string) error {
	c, _, err := m.client()
	if err != nil {
		return err
	}
	return c.do("DELETE", "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), nil, nil)
}

// DeleteRemoteTunnel deletes another tunnel of the account (never the one
// this machine runs).
func (m *Manager) DeleteRemoteTunnel(id string) error {
	c, ref, err := m.client()
	if err != nil {
		return err
	}
	if id == ref.TunnelID {
		return fmt.Errorf("das ist der Tunnel dieses Rechners")
	}
	path := "/accounts/" + url.PathEscape(ref.AccountID) + "/cfd_tunnel/" + url.PathEscape(id)
	// Stale connections of a server that is gone block the delete; drop them first.
	_ = c.do("DELETE", path+"/connections", nil, nil)
	return c.do("DELETE", path, nil, nil)
}

// --- remote config helpers ---

func remoteIngress(cfg map[string]interface{}) []map[string]interface{} {
	list, _ := cfg["ingress"].([]interface{})
	var out []map[string]interface{}
	for _, item := range list {
		if r, ok := item.(map[string]interface{}); ok {
			out = append(out, r)
		}
	}
	return out
}

func rulesFromRemote(cfg map[string]interface{}) []IngressRule {
	rules := []IngressRule{}
	for _, r := range remoteIngress(cfg) {
		rule := IngressRule{Hostname: strings.ToLower(str(r["hostname"])), Path: str(r["path"]), Service: str(r["service"])}
		if rule.Hostname == "" && rule.Path == "" {
			continue // catch-all
		}
		if o, ok := r["originRequest"].(map[string]interface{}); ok {
			rule.NoTLSVerify, _ = o["noTLSVerify"].(bool)
		}
		rules = append(rules, rule)
	}
	return rules
}

func str(v interface{}) string {
	s, _ := v.(string)
	return s
}

// normalizeJSON round-trips v through JSON so values built in Go compare
// equal to the ones decoded from the API.
func normalizeJSON(v interface{}) interface{} {
	data, _ := json.Marshal(v)
	var out interface{}
	_ = json.Unmarshal(data, &out)
	return out
}

// --- API client ---

type cfClient struct {
	token string
}

type zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func (c *cfClient) do(method, path string, body, out interface{}) error {
	_, err := c.doPage(method, path, body, out)
	return err
}

// doPage performs one API call and returns the total page count.
func (c *cfClient) doPage(method, path string, body, out interface{}) (int, error) {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, apiBase+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Cloudflare nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	var env struct {
		Success bool `json:"success"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
		Result     json.RawMessage `json:"result"`
		ResultInfo struct {
			TotalPages int `json:"total_pages"`
		} `json:"result_info"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return 0, fmt.Errorf("Cloudflare-API %s: HTTP %d", resp.Status, resp.StatusCode)
	}
	if !env.Success {
		var msgs []string
		for _, e := range env.Errors {
			msgs = append(msgs, fmt.Sprintf("%s (%d)", e.Message, e.Code))
		}
		if len(msgs) == 0 {
			msgs = append(msgs, resp.Status)
		}
		return 0, fmt.Errorf("Cloudflare: %s", strings.Join(msgs, "; "))
	}
	if out != nil && len(env.Result) > 0 && string(env.Result) != "null" {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return 0, fmt.Errorf("Cloudflare-Antwort: %w", err)
		}
	}
	return env.ResultInfo.TotalPages, nil
}

type apiTunnel struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Status      string            `json:"status"`
	CreatedAt   string            `json:"created_at"`
	Connections []json.RawMessage `json:"connections"`
}

func (t apiTunnel) toRemote() RemoteTunnel {
	return RemoteTunnel{ID: t.ID, Name: t.Name, Status: t.Status, Connections: len(t.Connections), CreatedAt: t.CreatedAt}
}

func (c *cfClient) tunnel(ref RemoteRef) (RemoteTunnel, error) {
	var t apiTunnel
	err := c.do("GET", "/accounts/"+url.PathEscape(ref.AccountID)+"/cfd_tunnel/"+url.PathEscape(ref.TunnelID), nil, &t)
	return t.toRemote(), err
}

func (c *cfClient) tunnels(ref RemoteRef) ([]RemoteTunnel, error) {
	var list []apiTunnel
	if err := c.do("GET", "/accounts/"+url.PathEscape(ref.AccountID)+"/cfd_tunnel?is_deleted=false&per_page=100", nil, &list); err != nil {
		return nil, err
	}
	var out []RemoteTunnel
	for _, t := range list {
		out = append(out, t.toRemote())
	}
	return out, nil
}

func (c *cfClient) tunnelConfig(ref RemoteRef) (map[string]interface{}, error) {
	var res struct {
		Config map[string]interface{} `json:"config"`
	}
	err := c.do("GET", "/accounts/"+url.PathEscape(ref.AccountID)+"/cfd_tunnel/"+url.PathEscape(ref.TunnelID)+"/configurations", nil, &res)
	if res.Config == nil {
		res.Config = map[string]interface{}{}
	}
	return res.Config, err
}

func (c *cfClient) putTunnelConfig(ref RemoteRef, cfg map[string]interface{}) error {
	return c.do("PUT", "/accounts/"+url.PathEscape(ref.AccountID)+"/cfd_tunnel/"+url.PathEscape(ref.TunnelID)+"/configurations",
		map[string]interface{}{"config": cfg}, nil)
}

func (c *cfClient) zones() ([]zone, error) {
	var all []zone
	for page := 1; ; page++ {
		var list []zone
		pages, err := c.doPage("GET", fmt.Sprintf("/zones?per_page=50&page=%d", page), nil, &list)
		if err != nil {
			return nil, err
		}
		all = append(all, list...)
		if page >= pages {
			return all, nil
		}
	}
}

func (c *cfClient) dnsRecords(z zone, q url.Values) ([]DNSRecord, error) {
	var all []DNSRecord
	for page := 1; ; page++ {
		q.Set("per_page", "500")
		q.Set("page", fmt.Sprint(page))
		var list []DNSRecord
		pages, err := c.doPage("GET", "/zones/"+url.PathEscape(z.ID)+"/dns_records?"+q.Encode(), nil, &list)
		if err != nil {
			return nil, err
		}
		for i := range list {
			list[i].ZoneID, list[i].ZoneName = z.ID, z.Name
		}
		all = append(all, list...)
		if page >= pages {
			return all, nil
		}
	}
}

// zoneFor finds the zone a hostname belongs to (longest matching suffix).
func zoneFor(zones []zone, host string) (zone, bool) {
	host = strings.TrimPrefix(host, "*.")
	best := zone{}
	for _, z := range zones {
		n := strings.ToLower(z.Name)
		if (host == n || strings.HasSuffix(host, "."+n)) && len(n) > len(best.Name) {
			best = z
		}
	}
	return best, best.ID != ""
}

func (c *cfClient) ensureDNS(zones []zone, host, target string, rep *SyncReport) error {
	z, ok := zoneFor(zones, host)
	if !ok {
		return fmt.Errorf("die Domain liegt nicht in deinem Cloudflare-Konto (oder der Token darf sie nicht bearbeiten)")
	}
	recs, err := c.dnsRecords(z, url.Values{"name": {host}})
	if err != nil {
		return err
	}
	have := false
	for _, r := range recs {
		switch {
		case r.Type == "CNAME" && strings.EqualFold(r.Content, target):
			have = true
		case r.Type == "A" || r.Type == "AAAA" || r.Type == "CNAME":
			if err := c.do("DELETE", "/zones/"+url.PathEscape(z.ID)+"/dns_records/"+url.PathEscape(r.ID), nil, nil); err != nil {
				return err
			}
			rep.Changes = append(rep.Changes, fmt.Sprintf("DNS %s: alten Eintrag %s %s entfernt", host, r.Type, r.Content))
		}
	}
	if have {
		return nil
	}
	err = c.do("POST", "/zones/"+url.PathEscape(z.ID)+"/dns_records", map[string]interface{}{
		"type": "CNAME", "name": host, "content": target, "proxied": true, "ttl": 1,
		"comment": "Hangar Tunnel",
	}, nil)
	if err == nil {
		rep.Changes = append(rep.Changes, fmt.Sprintf("DNS %s → Tunnel angelegt", host))
	}
	return err
}

func (c *cfClient) removeDNS(zones []zone, host, target string, rep *SyncReport) error {
	z, ok := zoneFor(zones, host)
	if !ok {
		return nil
	}
	recs, err := c.dnsRecords(z, url.Values{"name": {host}, "type": {"CNAME"}})
	if err != nil {
		return err
	}
	for _, r := range recs {
		if strings.EqualFold(r.Content, target) {
			if err := c.do("DELETE", "/zones/"+url.PathEscape(z.ID)+"/dns_records/"+url.PathEscape(r.ID), nil, nil); err != nil {
				return err
			}
			rep.Changes = append(rep.Changes, fmt.Sprintf("DNS %s entfernt", host))
		}
	}
	return nil
}
