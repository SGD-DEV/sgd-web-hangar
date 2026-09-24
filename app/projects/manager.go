package projects

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/dns"
	"github.com/devour-app/devour/app/phpfcgi"
	"github.com/devour-app/devour/app/services"
)

// validDomain matches a single hostname label or dot-joined labels. We accept
// only what RFC 1123 already allows: lowercase letters, digits, hyphens (not
// leading/trailing), labels separated by dots, total length capped at 253.
//
// This is a hard gate: the domain becomes a filename (vhost .conf), an entry
// in the hosts file, an argument to PowerShell during elevated hosts edits,
// and a server_name in nginx/apache configs. Letting "../" or quote chars
// through any one of those paths is a real injection surface.
var validDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

func validateDomain(d string) error {
	if d == "" {
		return fmt.Errorf("domain is required")
	}
	if len(d) > 253 {
		return fmt.Errorf("domain too long (max 253 chars)")
	}
	if !validDomain.MatchString(strings.ToLower(d)) {
		return fmt.Errorf("domain %q is not a valid hostname (use lowercase letters, digits, hyphens, and dots only)", d)
	}
	return nil
}

// SSLCertGenerator is an interface for generating SSL certificates
type SSLCertGenerator interface {
	GenerateCert(domain string) error
	GetCert(domain string) (SSLCertInfo, error)
}

type SSLCertInfo struct {
	CertPath string
	KeyPath  string
}

type Manager struct {
	paths          config.Paths
	store          *config.Store
	serviceManager *services.Manager
	detector       *Detector
	sslGenerator   SSLCertGenerator
	// configChanged is set whenever a vhost file is written with new
	// content or removed; the next reload restarts running web servers.
	configChanged atomic.Bool
}

func NewManager(paths config.Paths, store *config.Store, svcMgr *services.Manager) *Manager {
	return &Manager{
		paths:          paths,
		store:          store,
		serviceManager: svcMgr,
		detector:       NewDetector(),
	}
}

// SetSSLGenerator sets the SSL certificate generator (called after SSL manager is created)
func (m *Manager) SetSSLGenerator(gen SSLCertGenerator) {
	m.sslGenerator = gen
}

func (m *Manager) Create(name, path, domain string) (Project, error) {
	return m.CreateWithOptions(CreateOptions{Name: name, Path: path, Domain: domain})
}

// CreateOptions packages every knob the project-create flow needs. New
// fields can be added without breaking existing Wails bindings since the
// frontend just sends the fields it knows about.
type CreateOptions struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Domain      string `json:"domain"`
	Framework   string `json:"framework,omitempty"`    // override auto-detect
	ProxyTarget string `json:"proxy_target,omitempty"` // required when Framework == "proxy"
	WebServer   string `json:"web_server,omitempty"`   // "apache" | "nginx", default apache
	SSLEnabled  bool   `json:"ssl_enabled,omitempty"`  // generate https vhost + mkcert
	LocalOnly   bool   `json:"local_only,omitempty"`   // only reachable from this machine
}

// CreateWithOptions is the rich-form project creator. Use Create() for the
// simple legacy "give me a PHP project from this directory" path.
func (m *Manager) CreateWithOptions(opts CreateOptions) (Project, error) {
	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return Project{}, fmt.Errorf("projects: reading config: %w", err)
	}

	name := opts.Name
	if name == "" {
		return Project{}, fmt.Errorf("projects: name required")
	}
	domain := strings.ToLower(opts.Domain)
	if domain == "" {
		domain = strings.ToLower(name) + ".test"
	}
	if err := validateDomain(domain); err != nil {
		return Project{}, fmt.Errorf("projects: %w", err)
	}
	if err := m.checkHostnamesFree(name, []string{domain}); err != nil {
		return Project{}, err
	}
	if err := validateConfigPath(opts.Path); err != nil {
		return Project{}, err
	}

	path := opts.Path
	if path == "" && opts.Framework != string(FrameworkProxy) {
		// Proxy projects don't need an on-disk directory - they forward to
		// an external service. Everything else does.
		root := m.projectsRoot(cfg)
		path = filepath.Join(root, name)
	}

	if path != "" {
		if err := os.MkdirAll(path, 0755); err != nil {
			return Project{}, fmt.Errorf("projects: creating directory %s: %w", path, err)
		}
	}

	var framework Framework
	var docRoot string
	if opts.Framework == string(FrameworkProxy) {
		target, err := validateProxyTarget(opts.ProxyTarget)
		if err != nil {
			return Project{}, err
		}
		opts.ProxyTarget = target
		framework = FrameworkProxy
		docRoot = "" // unused for proxy
	} else if opts.Framework != "" {
		framework = Framework(opts.Framework)
		docRoot = m.detector.GetDocumentRoot(path, framework)
	} else {
		framework = m.detector.Detect(path)
		docRoot = m.detector.GetDocumentRoot(path, framework)
	}

	webServer := opts.WebServer
	if webServer == "" {
		webServer = "apache"
	}
	project := Project{
		Name:         name,
		Path:         path,
		Domain:       domain,
		PHPVersion:   cfg.ActivePHP,
		WebServer:    webServer,
		SSLEnabled:   opts.SSLEnabled,
		Framework:    string(framework),
		DocumentRoot: docRoot,
		ProxyTarget:  opts.ProxyTarget,
		LocalOnly:    opts.LocalOnly,
		CreatedAt:    time.Now().Format(time.RFC3339),
	}

	data, err := json.Marshal(project)
	if err != nil {
		return Project{}, fmt.Errorf("projects: marshaling: %w", err)
	}

	if err := m.store.SaveProject(name, data); err != nil {
		return Project{}, fmt.Errorf("projects: saving: %w", err)
	}

	// Generate vhost configs and add hosts entry
	m.linkProject(project, cfg)

	return project, nil
}

func (m *Manager) Delete(name string) error {
	project, err := m.Get(name)
	var unlinkErr error
	if err == nil {
		unlinkErr = m.unlinkProject(project)
	}
	if err := m.store.DeleteProject(name); err != nil {
		return fmt.Errorf("projects: deleting %s: %w", name, err)
	}
	// Drop the removed vhost from the running web server.
	m.reloadWebServersIfRunning()
	// DB row is gone — surface any unlink residue so the user knows a
	// vhost file or hosts entry may still be hanging around. We don't
	// abort deletion on this: leaving the project row but having a
	// partially-removed vhost is worse than the reverse.
	if unlinkErr != nil {
		return fmt.Errorf("projects: deleted %s but cleanup incomplete: %w", name, unlinkErr)
	}
	return nil
}

func (m *Manager) Get(name string) (Project, error) {
	data, err := m.store.GetProject(name)
	if err != nil {
		return Project{}, fmt.Errorf("projects: getting %s: %w", name, err)
	}

	var project Project
	if err := json.Unmarshal(data, &project); err != nil {
		return Project{}, fmt.Errorf("projects: unmarshaling %s: %w", name, err)
	}

	return project, nil
}

func (m *Manager) List() []Project {
	allData, err := m.store.GetAllProjects()
	if err != nil {
		return nil
	}

	projects := make([]Project, 0, len(allData))
	for _, data := range allData {
		var p Project
		if err := json.Unmarshal(data, &p); err == nil {
			projects = append(projects, p)
		}
	}

	return projects
}

func (m *Manager) Scan() ([]Project, error) {
	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return nil, fmt.Errorf("projects: reading config: %w", err)
	}

	root := m.projectsRoot(cfg)
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("projects: creating projects root: %w", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("projects: scanning %s: %w", root, err)
	}

	var discovered []Project
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		projPath := filepath.Join(root, entry.Name())

		// Check if already registered
		if existing, err := m.Get(entry.Name()); err == nil {
			// Already registered — fix stale paths if needed
			needsUpdate := false
			if existing.Path != projPath {
				existing.Path = projPath
				needsUpdate = true
			}
			// Recalculate DocumentRoot based on actual path
			framework := m.detector.Detect(projPath)
			correctDocRoot := m.detector.GetDocumentRoot(projPath, framework)
			if existing.DocumentRoot != correctDocRoot {
				existing.DocumentRoot = correctDocRoot
				existing.Framework = string(framework)
				needsUpdate = true
			}
			// Set PHP version if missing
			if existing.PHPVersion == "" && cfg.ActivePHP != "" {
				existing.PHPVersion = cfg.ActivePHP
				needsUpdate = true
			}
			if needsUpdate {
				m.Update(existing.Name, existing)
			}
			// Always regenerate vhost configs to keep PHP CGI paths current
			m.linkProject(existing, cfg)
			continue
		}

		framework := m.detector.Detect(projPath)
		docRoot := m.detector.GetDocumentRoot(projPath, framework)
		domain := strings.ToLower(entry.Name()) + ".test"

		p := Project{
			Name:         entry.Name(),
			Path:         projPath,
			Domain:       domain,
			PHPVersion:   cfg.ActivePHP,
			WebServer:    "apache",
			Framework:    string(framework),
			DocumentRoot: docRoot,
			CreatedAt:    time.Now().Format(time.RFC3339),
		}

		// Save to DB
		data, _ := json.Marshal(p)
		m.store.SaveProject(entry.Name(), data)

		// Generate vhost configs and add hosts entry
		m.linkProject(p, cfg)

		discovered = append(discovered, p)
	}

	return discovered, nil
}

func (m *Manager) Update(name string, project Project) error {
	data, err := json.Marshal(project)
	if err != nil {
		return fmt.Errorf("projects: marshaling %s: %w", name, err)
	}

	return m.store.SaveProject(name, data)
}

// ProjectSettings is everything the "Edit project" dialog can change. The
// project name is the storage key and stays fixed.
type ProjectSettings struct {
	Domain       string   `json:"domain"`
	Aliases      []string `json:"aliases"`
	Path         string   `json:"path"`
	DocumentRoot string   `json:"document_root"`
	PHPVersion   string   `json:"php_version"`
	ProxyTarget  string   `json:"proxy_target"`
	SSLEnabled   bool     `json:"ssl_enabled"`
	LocalOnly    bool     `json:"local_only"`
}

// UpdateSettings validates and applies an edit to an existing project and
// rewrites its vhosts. When the local domain changes, the old vhost files and
// hosts entry are removed first so nothing stale keeps answering.
func (m *Manager) UpdateSettings(name string, s ProjectSettings) (Project, error) {
	p, err := m.Get(name)
	if err != nil {
		return Project{}, err
	}
	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return Project{}, fmt.Errorf("projects: reading config: %w", err)
	}

	domain := strings.ToLower(strings.TrimSpace(s.Domain))
	if err := validateDomain(domain); err != nil {
		return Project{}, fmt.Errorf("projects: %w", err)
	}
	aliases, err := normalizeAliases(s.Aliases, domain)
	if err != nil {
		return Project{}, err
	}
	if err := m.checkHostnamesFree(name, append([]string{domain}, aliases...)); err != nil {
		return Project{}, err
	}

	updated := p
	updated.Domain = domain
	updated.Aliases = aliases
	updated.SSLEnabled = s.SSLEnabled
	updated.LocalOnly = s.LocalOnly

	if p.Framework == string(FrameworkProxy) {
		target, err := validateProxyTarget(s.ProxyTarget)
		if err != nil {
			return Project{}, err
		}
		updated.ProxyTarget = target
	} else {
		path := strings.TrimSpace(s.Path)
		if path == "" {
			path = p.Path
		}
		if err := validateConfigPath(path); err != nil {
			return Project{}, err
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return Project{}, fmt.Errorf("projects: folder %s does not exist", path)
		}
		updated.Path = path

		docRoot := strings.TrimSpace(s.DocumentRoot)
		if docRoot == "" || (path != p.Path && docRoot == p.DocumentRoot) {
			docRoot = m.detector.GetDocumentRoot(path, Framework(p.Framework))
		}
		if err := validateConfigPath(docRoot); err != nil {
			return Project{}, err
		}
		if info, err := os.Stat(docRoot); err != nil || !info.IsDir() {
			return Project{}, fmt.Errorf("projects: document root %s does not exist", docRoot)
		}
		updated.DocumentRoot = docRoot

		if s.PHPVersion != "" {
			if _, err := os.Stat(filepath.Join(m.paths.PHPPath(s.PHPVersion), "php.exe")); err != nil {
				return Project{}, fmt.Errorf("projects: PHP %s is not installed", s.PHPVersion)
			}
			updated.PHPVersion = s.PHPVersion
		}
	}

	if domain != p.Domain {
		// Certificates are issued per domain; the new one gets its own on
		// the next EnsureProjectsReady if SSL stays enabled.
		updated.SSLCertPath = ""
		updated.SSLKeyPath = ""
		if err := m.unlinkProject(p); err != nil {
			// Not fatal: a leftover hosts line is harmless, the vhost
			// files were removed first.
			fmt.Fprintf(os.Stderr, "projects: unlinking old domain %s: %v\n", p.Domain, err)
		}
	}
	if !updated.SSLEnabled {
		updated.SSLCertPath = ""
		updated.SSLKeyPath = ""
	}

	if err := m.Update(name, updated); err != nil {
		return Project{}, err
	}
	m.linkProject(updated, cfg)
	return updated, nil
}

// checkHostnamesFree makes sure no other project already answers to one of
// hosts - two vhosts with the same name would silently shadow each other.
func (m *Manager) checkHostnamesFree(self string, hosts []string) error {
	taken := map[string]string{}
	for _, other := range m.List() {
		if other.Name == self {
			continue
		}
		taken[other.Domain] = other.Name
		for _, a := range other.Aliases {
			taken[a] = other.Name
		}
	}
	for _, h := range hosts {
		if owner, ok := taken[h]; ok {
			return fmt.Errorf("projects: %s is already used by project %q", h, owner)
		}
	}
	return nil
}

func normalizeAliases(in []string, domain string) ([]string, error) {
	seen := map[string]bool{domain: true}
	var out []string
	for _, a := range in {
		a = strings.ToLower(strings.TrimSpace(a))
		a = strings.TrimPrefix(strings.TrimPrefix(a, "https://"), "http://")
		a = strings.TrimSuffix(a, "/")
		if a == "" || seen[a] {
			continue
		}
		if err := validateDomain(a); err != nil {
			return nil, fmt.Errorf("projects: alias: %w", err)
		}
		seen[a] = true
		out = append(out, a)
	}
	return out, nil
}

// validateProxyTarget accepts http(s)://host[:port][/path]. The value is
// pasted into Apache and Nginx configs, so anything that could break out of
// the directive (quotes, spaces, semicolons, newlines) is rejected.
func validateProxyTarget(raw string) (string, error) {
	t := strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(t)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("projects: proxy target must look like http://127.0.0.1:8000")
	}
	if strings.ContainsAny(t, " \t\r\n\"';{}") {
		return "", fmt.Errorf("projects: proxy target contains invalid characters")
	}
	return t, nil
}

// validateConfigPath rejects paths that would break the quoted path
// directives in the generated web server configs.
func validateConfigPath(p string) error {
	if strings.ContainsAny(p, "\"\r\n;{}") {
		return fmt.Errorf("projects: path %q contains characters that are not allowed", p)
	}
	return nil
}

// SiteLogKind is one of the per-vhost log streams Apache/Nginx write.
// File naming convention (set in vhost templates): "<domain>_<kind>.log"
//
// Examples for project "blog" with domain "blog.test":
//
//	access      → data/logs/blog.test_access.log
//	error       → data/logs/blog.test_error.log
//	ssl_access  → data/logs/blog.test_ssl_access.log
//	ssl_error   → data/logs/blog.test_ssl_error.log
type SiteLogKind string

const (
	SiteLogAccess    SiteLogKind = "access"
	SiteLogError     SiteLogKind = "error"
	SiteLogSSLAccess SiteLogKind = "ssl_access"
	SiteLogSSLError  SiteLogKind = "ssl_error"
)

// SiteLogFile describes one log file available for a project.
type SiteLogFile struct {
	Kind    SiteLogKind `json:"kind"`
	Path    string      `json:"path"`
	Size    int64       `json:"size"`
	ModTime string      `json:"mod_time"`
}

// ListSiteLogs returns the log files that exist on disk for a project,
// keyed by their kind. Missing kinds (e.g. SSL logs for a non-SSL site)
// are simply omitted.
func (m *Manager) ListSiteLogs(projectName string) ([]SiteLogFile, error) {
	p, err := m.Get(projectName)
	if err != nil {
		return nil, err
	}
	logDir := m.paths.LogsPath()
	kinds := []SiteLogKind{SiteLogAccess, SiteLogError, SiteLogSSLAccess, SiteLogSSLError}
	out := make([]SiteLogFile, 0, len(kinds))
	for _, k := range kinds {
		path := filepath.Join(logDir, fmt.Sprintf("%s_%s.log", p.Domain, k))
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		out = append(out, SiteLogFile{
			Kind:    k,
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339),
		})
	}
	return out, nil
}

// ReadSiteLog returns the last `lines` lines of the requested per-vhost log
// file for the project. Returns an empty slice (not an error) when the file
// doesn't exist yet — typical for a fresh project that has had no traffic or
// for SSL logs on an HTTP-only site.
//
// To bound memory we cap the read at 5 MB from the end of the file. Access
// logs in dev rarely exceed this; error logs almost never do.
func (m *Manager) ReadSiteLog(projectName string, kind SiteLogKind, lines int) ([]string, error) {
	if lines <= 0 {
		lines = 100
	}
	switch kind {
	case SiteLogAccess, SiteLogError, SiteLogSSLAccess, SiteLogSSLError:
	default:
		return nil, fmt.Errorf("projects: invalid log kind %q (want access|error|ssl_access|ssl_error)", kind)
	}

	p, err := m.Get(projectName)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(m.paths.LogsPath(), fmt.Sprintf("%s_%s.log", p.Domain, kind))

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("projects: opening %s: %w", path, err)
	}
	defer f.Close()

	const maxRead = 5 * 1024 * 1024
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("projects: stat %s: %w", path, err)
	}
	readSize := info.Size()
	offset := int64(0)
	if readSize > maxRead {
		offset = readSize - maxRead
		readSize = maxRead
	}
	buf := make([]byte, readSize)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return nil, fmt.Errorf("projects: reading %s: %w", path, err)
	}

	all := strings.Split(strings.TrimRight(string(buf), "\r\n"), "\n")
	if offset > 0 && len(all) > 0 {
		// First line was likely sliced mid-line by the offset cut — drop it.
		all = all[1:]
	}
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	for i, line := range all {
		all[i] = strings.TrimRight(line, "\r")
	}
	return all, nil
}

// projectsRoot returns the configured projects root, falling back to paths.ProjectsPath()
// If the configured root doesn't exist on disk, falls back to the exe-relative path.
func (m *Manager) projectsRoot(cfg config.AppConfig) string {
	if cfg.ProjectsRoot != "" {
		if _, err := os.Stat(cfg.ProjectsRoot); err == nil {
			return cfg.ProjectsRoot
		}
	}
	return m.paths.ProjectsPath()
}

// EnsureProjectsReady scans for projects, ensures vhost configs, hosts entries, and SSL certs.
// Called automatically before Apache/Nginx starts.
func (m *Manager) EnsureProjectsReady() error {
	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return fmt.Errorf("projects: reading config: %w", err)
	}

	// 1. Scan for new projects in the projects root
	m.Scan()

	// 2. Get all registered projects
	allProjects := m.List()
	if len(allProjects) == 0 {
		return nil
	}

	// 3. For each project, fix stale paths, ensure vhost configs exist, collect domains
	root := m.projectsRoot(cfg)
	var domainsNeedingHosts []string
	for _, p := range allProjects {
		// Fix stale project paths: if the stored Path doesn't exist but project dir
		// exists under current projects root, update it
		if _, err := os.Stat(p.Path); os.IsNotExist(err) {
			correctPath := filepath.Join(root, p.Name)
			if _, err := os.Stat(correctPath); err == nil {
				p.Path = correctPath
				// Also fix DocumentRoot if it was under the old path
				framework := m.detector.Detect(correctPath)
				p.DocumentRoot = m.detector.GetDocumentRoot(correctPath, framework)
				m.Update(p.Name, p)
			}
		}
		// Also ensure DocumentRoot is correct relative to project Path
		if p.DocumentRoot == "" || (!strings.HasPrefix(filepath.ToSlash(p.DocumentRoot), filepath.ToSlash(p.Path)) && !strings.HasPrefix(filepath.ToSlash(p.DocumentRoot), filepath.ToSlash(root))) {
			framework := m.detector.Detect(p.Path)
			p.DocumentRoot = m.detector.GetDocumentRoot(p.Path, framework)
			m.Update(p.Name, p)
		}

		// Regenerate vhost config (in case ports/paths changed)
		m.linkProject(p, cfg)

		// Check if hosts entry exists
		if !dns.HasHostEntry(p.Domain) {
			domainsNeedingHosts = append(domainsNeedingHosts, p.Domain)
		}

		// Generate SSL cert if SSL is enabled (either globally OR on this
		// individual project) and cert doesn't exist yet. Per-project enable
		// is the new flow - users toggle a lock icon on each project; the
		// global flag stays as a "default for new projects" knob.
		if (cfg.SSLEnabled || p.SSLEnabled) && m.sslGenerator != nil {
			certInfo, err := m.sslGenerator.GetCert(p.Domain)
			if err != nil || certInfo.CertPath == "" {
				// Generate cert
				if genErr := m.sslGenerator.GenerateCert(p.Domain); genErr == nil {
					// Update project with SSL paths
					if newCert, err := m.sslGenerator.GetCert(p.Domain); err == nil {
						p.SSLEnabled = true
						p.SSLCertPath = newCert.CertPath
						p.SSLKeyPath = newCert.KeyPath
						m.Update(p.Name, p)
						// Regenerate vhost with SSL paths
						m.linkProject(p, cfg)
					}
				}
			} else {
				// Cert exists — ensure project has SSL paths set
				if !p.SSLEnabled || p.SSLCertPath == "" {
					p.SSLEnabled = true
					p.SSLCertPath = certInfo.CertPath
					p.SSLKeyPath = certInfo.KeyPath
					m.Update(p.Name, p)
					m.linkProject(p, cfg)
				}
			}
		}
	}

	// 4. Batch-add hosts entries (single admin prompt if needed)
	if len(domainsNeedingHosts) > 0 {
		if err := dns.AddHostEntries(domainsNeedingHosts, "127.0.0.1"); err != nil {
			return fmt.Errorf("projects: adding hosts entries: %w", err)
		}
	}

	// 5. Tell any running web server to reload its config so the new/edited
	// vhost takes effect immediately. Without this the user has to manually
	// click Restart on Apache/Nginx after every project change. We only
	// reload services that are RUNNING - no point starting a service the
	// user hadn't started themselves.
	m.reloadWebServersIfRunning()

	return nil
}

// reloadWebServersIfRunning iterates over registered web-server services and,
// for each one currently in the Running state, sends a config reload by
// stopping and starting it. We don't have a soft-reload primitive on the
// service interface, but a Stop+Start with the existing data dirs preserved
// is fast (sub-second on Apache, ~1s on Nginx) and avoids the user having
// to remember to click Restart.
//
// Safe to call multiple times - if nothing's running it's a no-op. Only
// restarts when a vhost file actually changed since the last reload: every
// restart briefly takes all sites offline.
func (m *Manager) reloadWebServersIfRunning() {
	if m.serviceManager == nil || !m.configChanged.Swap(false) {
		return
	}
	for _, name := range []string{"apache", "nginx"} {
		st, err := m.serviceManager.Status(name)
		if err != nil || st.Status != services.StatusRunning {
			continue
		}
		// Restart will Stop -> Start. The web-server-conflict check in
		// services.Manager.Start has the OTHER server's status checked
		// for "running" before we issue this Stop, so a Restart of nginx
		// while apache is also running won't deadlock.
		_ = m.serviceManager.Restart(name)
	}
}

// RelinkAll rewrites the vhost files of every registered project. Run at
// startup so configs generated by an older Hangar (different templates,
// ports, PHP wiring) never reach a web server.
func (m *Manager) RelinkAll() {
	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return
	}
	for _, p := range m.List() {
		m.linkProject(p, cfg)
	}
}

// GetProjectsRoot returns the effective projects root directory
func (m *Manager) GetProjectsRoot() string {
	cfg, err := m.store.GetAppConfig()
	if err != nil {
		return m.paths.ProjectsPath()
	}
	return m.projectsRoot(cfg)
}

// phpUpstream returns the FastCGI pool name (see app/phpfcgi) a project's
// PHP requests go to: its pinned version if installed, else the active one.
func (m *Manager) phpUpstream(project Project, cfg config.AppConfig) string {
	for _, v := range []string{project.PHPVersion, cfg.ActivePHP} {
		if v == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(m.paths.PHPPath(v), "php-cgi.exe")); err == nil {
			return phpfcgi.UpstreamName(v)
		}
	}
	return ""
}

// linkProject writes Apache vhost + Nginx site configs and adds a hosts file entry
func (m *Manager) linkProject(project Project, cfg config.AppConfig) {
	docRoot := filepath.ToSlash(project.DocumentRoot)
	logDir := filepath.ToSlash(m.paths.LogsPath())
	port := cfg.ApachePort
	if port == 0 {
		port = 80
	}
	phpUpstream := m.phpUpstream(project, cfg)
	proxyTarget := strings.TrimRight(project.ProxyTarget, "/")

	// Write Apache vhost config. If this is a proxy project, the template
	// emits ProxyPass/ProxyPassReverse instead of a DocumentRoot mount.
	isProxy := project.Framework == string(FrameworkProxy) && project.ProxyTarget != ""
	// Only emit the HTTPS block when we have BOTH the SSLEnabled flag AND
	// real cert paths. SSLEnabled with empty paths produces
	// `SSLCertificateFile ""` which Apache rejects with
	// "SSLCertificateFile takes one argument". The cert generator runs in
	// EnsureProjectsReady; until it's done, don't emit a broken vhost.
	sslReady := project.SSLEnabled && project.SSLCertPath != "" && project.SSLKeyPath != ""
	apacheVhostDir := filepath.Join(m.paths.ConfPath("apache"), "vhosts")
	os.MkdirAll(apacheVhostDir, 0755)
	apacheConf := filepath.Join(apacheVhostDir, domainToFileName(project.Domain))
	m.noteWrite(writeTemplate(apacheVhostTmpl, apacheConf, map[string]interface{}{
		"ServerName":   project.Domain,
		"Aliases":      project.Aliases,
		"DocumentRoot": docRoot,
		"Port":         port,
		"LogDir":       logDir,
		"SSLEnabled":   sslReady,
		"SSLCertPath":  filepath.ToSlash(project.SSLCertPath),
		"SSLKeyPath":   filepath.ToSlash(project.SSLKeyPath),
		"PHPUpstream":  phpUpstream,
		"LocalOnly":    project.LocalOnly,
		"IsProxy":      isProxy,
		"ProxyTarget":  proxyTarget,
	}))

	// Write Nginx site config
	nginxPort := cfg.NginxPort
	if nginxPort == 0 {
		nginxPort = 80
	}
	nginxSitesDir := filepath.Join(m.paths.ConfPath("nginx"), "sites")
	os.MkdirAll(nginxSitesDir, 0755)
	nginxConf := filepath.Join(nginxSitesDir, domainToFileName(project.Domain))

	// Find nginx prefix dir for fastcgi_params include
	prefixDir := ""
	if svc, err := m.serviceManager.Get("nginx"); err == nil {
		type nginxPather interface {
			GetPrefixDir() string
		}
		if np, ok := svc.(nginxPather); ok {
			prefixDir = filepath.ToSlash(np.GetPrefixDir())
		}
	}

	m.noteWrite(writeTemplate(nginxSiteTmpl, nginxConf, map[string]interface{}{
		"ServerName":   project.Domain,
		"Aliases":      project.Aliases,
		"DocumentRoot": docRoot,
		"Listen":       nginxPort,
		"PHPUpstream":  phpUpstream,
		"LocalOnly":    project.LocalOnly,
		"PrefixDir":    prefixDir,
		"LogDir":       logDir,
		"SSLEnabled":   sslReady, // same defensive gating as Apache
		"SSLCertPath":  filepath.ToSlash(project.SSLCertPath),
		"SSLKeyPath":   filepath.ToSlash(project.SSLKeyPath),
		"IsProxy":      isProxy,
		"ProxyTarget":  proxyTarget,
	}))

	// Add hosts file entry (requires admin — best effort)
	dns.AddHostEntry(project.Domain, "127.0.0.1")
}

// noteWrite records that a vhost file changed (see reloadWebServersIfRunning).
func (m *Manager) noteWrite(changed bool, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "projects: writing vhost: %v\n", err)
		m.configChanged.Store(true)
		return
	}
	if changed {
		m.configChanged.Store(true)
	}
}

// unlinkProject removes Apache vhost + Nginx site configs and hosts entry
func (m *Manager) unlinkProject(project Project) error {
	var problems []string

	apacheConf := filepath.Join(m.paths.ConfPath("apache"), "vhosts", domainToFileName(project.Domain))
	if err := os.Remove(apacheConf); err == nil {
		m.configChanged.Store(true)
	} else if !os.IsNotExist(err) {
		problems = append(problems, fmt.Sprintf("apache vhost: %v", err))
	}

	nginxConf := filepath.Join(m.paths.ConfPath("nginx"), "sites", domainToFileName(project.Domain))
	if err := os.Remove(nginxConf); err == nil {
		m.configChanged.Store(true)
	} else if !os.IsNotExist(err) {
		problems = append(problems, fmt.Sprintf("nginx site: %v", err))
	}

	if err := dns.RemoveHostEntry(project.Domain); err != nil {
		problems = append(problems, fmt.Sprintf("hosts entry: %v", err))
	}

	if len(problems) > 0 {
		return fmt.Errorf("unlinking %s: %s", project.Domain, strings.Join(problems, "; "))
	}
	return nil
}

func domainToFileName(domain string) string {
	return strings.ReplaceAll(domain, ".", "_") + ".conf"
}

// writeTemplate renders tmplStr and writes it to outPath only when the
// content differs from what is on disk. Returns whether the file changed, so
// callers restart a web server only when its config really moved.
func writeTemplate(tmplStr, outPath string, data interface{}) (bool, error) {
	tmpl, err := template.New("tmpl").Parse(tmplStr)
	if err != nil {
		return false, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return false, err
	}
	if old, err := os.ReadFile(outPath); err == nil && bytes.Equal(old, buf.Bytes()) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return false, err
	}
	return true, os.WriteFile(outPath, buf.Bytes(), 0644)
}

// Embedded vhost templates (same as service templates but project-aware).
//
// PHP is handed to the FastCGI worker pool of the project's PHP version
// (balancer://phpXY on Apache, upstream phpXY on Nginx); both are declared
// in the main server config by the web server service.
const apacheVhostTmpl = `{{define "body"}}    ServerName {{.ServerName}}
{{range .Aliases}}    ServerAlias {{.}}
{{end}}{{if .IsProxy}}    # Reverse-proxy to a non-PHP backend (Python, Node, Go, etc.)
    ProxyPreserveHost On
    ProxyRequests Off
    ProxyPass "/" "{{.ProxyTarget}}/" upgrade=websocket
    ProxyPassReverse "/" "{{.ProxyTarget}}/"
{{if .LocalOnly}}    <Location "/">
        Require local
    </Location>
{{end}}{{else}}    DocumentRoot "{{.DocumentRoot}}"

    <Directory "{{.DocumentRoot}}">
        Options FollowSymLinks
        AllowOverride All
        {{if .LocalOnly}}Require local{{else}}Require all granted{{end}}
{{if .PHPUpstream}}        <FilesMatch "\.php$">
            <If "-f %{REQUEST_FILENAME}">
                SetHandler "proxy:balancer://{{.PHPUpstream}}/"
            </If>
        </FilesMatch>
{{end}}    </Directory>
{{end}}{{end}}# HTTP
<VirtualHost *:{{.Port}}>
{{template "body" .}}
    ErrorLog "{{.LogDir}}/{{.ServerName}}_error.log"
    CustomLog "{{.LogDir}}/{{.ServerName}}_access.log" combined
</VirtualHost>
{{if .SSLEnabled}}
# HTTPS
<VirtualHost *:443>
{{template "body" .}}
    SSLEngine on
    SSLCertificateFile "{{.SSLCertPath}}"
    SSLCertificateKeyFile "{{.SSLKeyPath}}"

    ErrorLog "{{.LogDir}}/{{.ServerName}}_ssl_error.log"
    CustomLog "{{.LogDir}}/{{.ServerName}}_ssl_access.log" combined
</VirtualHost>
{{end}}`

const nginxSiteTmpl = `server {
    listen {{.Listen}};
{{if .SSLEnabled}}    listen 443 ssl;
    ssl_certificate     "{{.SSLCertPath}}";
    ssl_certificate_key "{{.SSLKeyPath}}";
{{end}}
    server_name {{.ServerName}}{{range .Aliases}} {{.}}{{end}};
{{if .LocalOnly}}
    allow 127.0.0.1;
    allow ::1;
    deny all;
{{end}}

    location ~ /\.(?!well-known) {
        deny all;
    }
{{if .IsProxy}}    # Reverse-proxy to a non-PHP backend (Python/Node/Go/etc.)
    location / {
        proxy_pass {{.ProxyTarget}};
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
{{else}}    root "{{.DocumentRoot}}";
    index index.php index.html index.htm;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }
{{if .PHPUpstream}}
    location ~ \.php$ {
        try_files      $uri =404;
        fastcgi_pass   {{.PHPUpstream}};
        fastcgi_next_upstream error timeout;
        fastcgi_read_timeout 300s;
        fastcgi_index  index.php;
        fastcgi_param  SCRIPT_FILENAME $document_root$fastcgi_script_name;
{{if .PrefixDir}}        include        "{{.PrefixDir}}/conf/fastcgi_params";{{end}}
    }
{{end}}{{end}}
    access_log "{{.LogDir}}/{{.ServerName}}_access.log";
    error_log  "{{.LogDir}}/{{.ServerName}}_error.log";
}
`