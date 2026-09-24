// Package phpfcgi runs PHP as a pool of FastCGI workers that both Apache
// (mod_proxy_fcgi + mod_proxy_balancer) and Nginx (upstream block) talk to.
//
// Why a pool of php-cgi.exe processes: on Windows php-cgi cannot fork
// children (PHP_FCGI_CHILDREN is ignored), so one process serves exactly one
// request at a time. Running N workers on N consecutive ports and letting the
// web server balance across them gives real concurrency.
//
// Why a supervisor: php-cgi exits on its own after PHP_FCGI_MAX_REQUESTS
// requests (default 500). Without a respawn loop PHP silently dies after a
// few hundred page views - fatal for a machine that is meant to run 24/7.
//
// This replaces the old Apache setup that exposed the PHP directory through
// ScriptAlias + Action (php-cgi.exe reachable as a CGI URL), which is the
// configuration the PHP manual explicitly warns against.
package phpfcgi

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devour-app/devour/app/config"
	"github.com/devour-app/devour/app/logs"
	"github.com/devour-app/devour/app/services"
)

const (
	// DefaultWorkers is used when AppConfig.PHPWorkers is unset.
	DefaultWorkers = 4
	// MaxWorkers bounds the port range a version may occupy (see PortFor).
	MaxWorkers = 8
	// maxRequests is handed to php-cgi as PHP_FCGI_MAX_REQUESTS. The worker
	// exits after that many requests and the supervisor starts a fresh one,
	// which also caps memory growth from leaky extensions.
	maxRequests = 1000
)

const logService = "php"

// PortFor returns the first FastCGI port for a PHP version. The mapping is
// deterministic ("8.3" -> 9830, "7.3" -> 9730) so generated web server
// configs stay stable no matter which versions are installed. A version
// occupies PortFor(v) .. PortFor(v)+MaxWorkers-1.
func PortFor(version string) int {
	major, minor := parseVersion(version)
	return 9000 + major*100 + minor*10
}

// UpstreamName is the identifier used for the Nginx upstream and the Apache
// balancer of a version, e.g. "php83".
func UpstreamName(version string) string {
	major, minor := parseVersion(version)
	return fmt.Sprintf("php%d%d", major, minor)
}

func parseVersion(version string) (int, int) {
	parts := strings.SplitN(version, ".", 3)
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if major < 0 || major > 9 {
		major = 0
	}
	if minor < 0 || minor > 9 {
		minor = 0
	}
	return major, minor
}

// Upstream describes one PHP version's worker ports for config templates.
type Upstream struct {
	Version string
	Name    string
	Ports   []int
}

// VersionStatus is what the UI shows per PHP version.
type VersionStatus struct {
	Version  string `json:"version"`
	Port     int    `json:"port"`
	Workers  int    `json:"workers"`
	Alive    int    `json:"alive"`
	Restarts int    `json:"restarts"`
}

type Pool struct {
	paths    config.Paths
	store    *config.Store
	logStore *logs.Store

	mu       sync.Mutex
	versions map[string]*versionPool
}

type versionPool struct {
	version string
	workers []*worker
}

type worker struct {
	port     int
	mu       sync.Mutex
	alive    bool
	stop     chan struct{}
	done     chan struct{}
	restarts int
}

func New(paths config.Paths, store *config.Store, logStore *logs.Store) *Pool {
	return &Pool{
		paths:    paths,
		store:    store,
		logStore: logStore,
		versions: make(map[string]*versionPool),
	}
}

// WorkerCount returns the configured number of workers per version.
func (p *Pool) WorkerCount() int {
	n := DefaultWorkers
	if cfg, err := p.store.GetAppConfig(); err == nil && cfg.PHPWorkers > 0 {
		n = cfg.PHPWorkers
	}
	if n > MaxWorkers {
		n = MaxWorkers
	}
	return n
}

// InstalledVersions lists PHP versions that have a php-cgi.exe on disk.
func (p *Pool) InstalledVersions() []string {
	dir := filepath.Join(p.paths.InstalledPath(), "php")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "php-cgi.exe")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Upstreams returns one entry per installed PHP version. Web server configs
// declare all of them so every vhost can reference its version; only the
// versions actually in use get running workers (see RequiredVersions).
func (p *Pool) Upstreams() []Upstream {
	n := p.WorkerCount()
	var out []Upstream
	for _, v := range p.InstalledVersions() {
		base := PortFor(v)
		ports := make([]int, n)
		for i := range ports {
			ports[i] = base + i
		}
		out = append(out, Upstream{Version: v, Name: UpstreamName(v), Ports: ports})
	}
	return out
}

// HasVersion reports whether php-cgi.exe exists for version.
func (p *Pool) HasVersion(version string) bool {
	if version == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(p.paths.PHPPath(version), "php-cgi.exe"))
	return err == nil
}

// RequiredVersions is the active PHP version plus every version a project
// is pinned to, filtered to what is actually installed.
func (p *Pool) RequiredVersions() []string {
	want := map[string]bool{}
	if cfg, err := p.store.GetAppConfig(); err == nil && cfg.ActivePHP != "" {
		want[cfg.ActivePHP] = true
	}
	if all, err := p.store.GetAllProjects(); err == nil {
		for _, raw := range all {
			var pr struct {
				PHPVersion string `json:"php_version"`
				Framework  string `json:"framework"`
			}
			if json.Unmarshal(raw, &pr) == nil && pr.PHPVersion != "" && pr.Framework != "proxy" {
				want[pr.PHPVersion] = true
			}
		}
	}
	var out []string
	for v := range want {
		if p.HasVersion(v) {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// EnsureRunning starts workers for every required version that isn't
// running yet, and restarts versions whose worker count changed.
func (p *Pool) EnsureRunning() error {
	versions := p.RequiredVersions()
	if len(versions) == 0 {
		p.logStore.AddWithLevel(logService, "No PHP version installed/active - .php requests will fail", "warn")
		return nil
	}
	n := p.WorkerCount()

	p.mu.Lock()
	defer p.mu.Unlock()
	var firstErr error
	for _, v := range versions {
		if vp, ok := p.versions[v]; ok {
			if len(vp.workers) == n {
				continue
			}
			vp.stopAll()
			delete(p.versions, v)
		}
		vp, err := p.startVersion(v, n)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		p.versions[v] = vp
	}
	return firstErr
}

// StopAll terminates every worker. Called when no web server is running.
func (p *Pool) StopAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for v, vp := range p.versions {
		vp.stopAll()
		delete(p.versions, v)
	}
	p.logStore.Add(logService, "PHP workers stopped")
}

// Status reports per-version worker health for the UI.
func (p *Pool) Status() []VersionStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]VersionStatus, 0, len(p.versions))
	for v, vp := range p.versions {
		st := VersionStatus{Version: v, Port: PortFor(v), Workers: len(vp.workers)}
		for _, w := range vp.workers {
			w.mu.Lock()
			if w.alive {
				st.Alive++
			}
			st.Restarts += w.restarts
			w.mu.Unlock()
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

func (p *Pool) startVersion(version string, n int) (*versionPool, error) {
	phpDir := p.paths.PHPPath(version)
	cgi := filepath.Join(phpDir, "php-cgi.exe")
	if _, err := os.Stat(cgi); err != nil {
		return nil, fmt.Errorf("php %s: php-cgi.exe not found in %s", version, phpDir)
	}
	vp := &versionPool{version: version}
	base := PortFor(version)
	for i := 0; i < n; i++ {
		w := &worker{port: base + i, stop: make(chan struct{}), done: make(chan struct{})}
		vp.workers = append(vp.workers, w)
		go p.supervise(version, cgi, phpDir, w)
	}
	p.logStore.Add(logService, fmt.Sprintf("PHP %s: started %d FastCGI workers on 127.0.0.1:%d-%d", version, n, base, base+n-1))
	return vp, nil
}

// supervise keeps one php-cgi worker alive until stop is closed. A worker
// that exits normally (max requests reached) is replaced immediately; one
// that keeps dying right after launch backs off up to 10s so a broken
// php.ini doesn't spin the CPU.
func (p *Pool) supervise(version, cgi, phpDir string, w *worker) {
	defer close(w.done)
	backoff := 250 * time.Millisecond
	for {
		cmd := exec.Command(cgi, "-b", fmt.Sprintf("127.0.0.1:%d", w.port))
		cmd.Dir = phpDir
		cmd.Env = append(os.Environ(),
			"PHPRC="+phpDir,
			fmt.Sprintf("PHP_FCGI_MAX_REQUESTS=%d", maxRequests),
		)
		services.HideWindow(cmd)
		started := time.Now()
		err := cmd.Start()
		if err == nil {
			w.setAlive(true)
			waitCh := make(chan error, 1)
			go func() { waitCh <- cmd.Wait() }()
			select {
			case <-w.stop:
				_ = cmd.Process.Kill()
				<-waitCh
				w.setAlive(false)
				return
			case err = <-waitCh:
			}
			w.setAlive(false)
		}
		select {
		case <-w.stop:
			return
		default:
		}
		w.mu.Lock()
		w.restarts++
		w.mu.Unlock()
		if time.Since(started) < 2*time.Second {
			p.logStore.AddWithLevel(logService, fmt.Sprintf("PHP %s worker :%d exited right after start (%v), retrying in %s", version, w.port, err, backoff), "warn")
			select {
			case <-w.stop:
				return
			case <-time.After(backoff):
			}
			if backoff < 10*time.Second {
				backoff *= 2
			}
		} else {
			backoff = 250 * time.Millisecond
		}
	}
}

func (w *worker) setAlive(v bool) {
	w.mu.Lock()
	w.alive = v
	w.mu.Unlock()
}

func (vp *versionPool) stopAll() {
	for _, w := range vp.workers {
		close(w.stop)
	}
	for _, w := range vp.workers {
		select {
		case <-w.done:
		case <-time.After(5 * time.Second):
		}
	}
}
