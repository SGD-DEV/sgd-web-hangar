package tunnel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devour-app/devour/app/config"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DEVOUR_HOME", dir)
	paths := config.NewPlatformPaths()
	if err := paths.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return NewManager(paths, store), dir
}

func TestSaveConfigKeepsUnknownKeysAndOriginRequest(t *testing.T) {
	m, _ := newTestManager(t)
	raw := `# my tunnel
tunnel: 1234
credentials-file: C:\creds\1234.json
protocol: http2 # keep me
ingress:
  - hostname: files.example.com
    service: http://127.0.0.1:8081
    originRequest:
      connectTimeout: 10s
  - service: http_status:404
`
	if err := os.MkdirAll(filepath.Dir(m.ConfigPath()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.ConfigPath(), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := m.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnel != "1234" || len(cfg.Ingress) != 2 {
		t.Fatalf("unexpected parse: %+v", cfg)
	}
	cfg.Ingress = append(cfg.Ingress[:1], IngressRule{Hostname: "Blog.Example.com", Service: "https://127.0.0.1:443", NoTLSVerify: true})
	if err := m.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(m.ConfigPath())
	s := string(out)
	for _, want := range []string{"# my tunnel", "protocol: http2", "# keep me", "connectTimeout: 10s", "hostname: blog.example.com", "noTLSVerify: true", "service: http_status:404"} {
		if !strings.Contains(s, want) {
			t.Errorf("saved config lacks %q:\n%s", want, s)
		}
	}
	if strings.Index(s, "http_status:404") < strings.Index(s, "blog.example.com") {
		t.Errorf("catch-all must be last:\n%s", s)
	}
	if _, err := os.Stat(m.ConfigPath() + ".bak"); err != nil {
		t.Errorf("no backup written: %v", err)
	}
}

func TestSaveConfigRejectsBadRules(t *testing.T) {
	m, _ := newTestManager(t)
	for _, rule := range []IngressRule{
		{Hostname: "bad host.com", Service: "http://127.0.0.1"},
		{Hostname: "ok.example.com", Service: ""},
	} {
		if err := m.SaveConfig(Config{Tunnel: "x", Ingress: []IngressRule{rule}}); err == nil {
			t.Errorf("expected error for %+v", rule)
		}
	}
}

func TestSaveConfigCreatesNewFile(t *testing.T) {
	m, _ := newTestManager(t)
	err := m.SaveConfig(Config{Tunnel: "abc", CredentialsFile: `C:\x\abc.json`, Ingress: []IngressRule{{Hostname: "a.example.com", Service: "http://127.0.0.1:80"}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := m.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnel != "abc" || cfg.CredentialsFile != `C:\x\abc.json` || len(cfg.Ingress) != 2 {
		t.Fatalf("round trip failed: %+v", cfg)
	}
}
