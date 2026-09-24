package projects

import "testing"

func TestValidateProxyTarget(t *testing.T) {
	good := map[string]string{
		"http://127.0.0.1:3000":   "http://127.0.0.1:3000",
		" https://localhost/app/": "https://localhost/app",
	}
	for in, want := range good {
		got, err := validateProxyTarget(in)
		if err != nil || got != want {
			t.Errorf("validateProxyTarget(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "127.0.0.1:3000", "ftp://x", `http://x/"; Require all granted`, "http://x/\nProxyPass", "http://x;"} {
		if _, err := validateProxyTarget(bad); err == nil {
			t.Errorf("validateProxyTarget(%q) accepted", bad)
		}
	}
}

func TestNormalizeAliases(t *testing.T) {
	got, err := normalizeAliases([]string{" WWW.Example.com ", "https://blog.example.com/", "site.test", "www.example.com", ""}, "site.test")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"www.example.com", "blog.example.com"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := normalizeAliases([]string{"bad_host!.com"}, "x.test"); err == nil {
		t.Error("invalid alias accepted")
	}
}

func TestValidateConfigPath(t *testing.T) {
	if err := validateConfigPath(`C:\Hosting\www\site`); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{`C:\a"b`, "C:\\a\nb", `C:\a;b`} {
		if validateConfigPath(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
