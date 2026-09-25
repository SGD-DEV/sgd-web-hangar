package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectDBName(t *testing.T) {
	for in, want := range map[string]string{
		"my-shop":               "my_shop",
		"2025site":              "p_2025site",
		"Blog.DE":               "blog_de",
		strings.Repeat("a", 40): strings.Repeat("a", 32),
	} {
		if got := projectDBName(in); got != want {
			t.Errorf("projectDBName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateGitURL(t *testing.T) {
	for _, ok := range []string{"https://github.com/user/repo.git", "https://gitea.example.com/org/site"} {
		if _, err := validateGitURL(ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://github.com/u/r", "git@github.com:u/r.git", "https://github.com", "https://tok@github.com/u/r", "file:///C:/x", "ssh://github.com/u/r"} {
		if _, err := validateGitURL(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if got := ProjectNameFromGitURL("https://github.com/User/My_Site.git"); got != "my-site" {
		t.Errorf("ProjectNameFromGitURL = %q", got)
	}
}

func TestExcludeSecretFiles(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git", "info"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("# git ls-files\n/.env"), 0644)
	excludeSecretFiles(dir)
	excludeSecretFiles(dir)
	data, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	s := string(data)
	if strings.Count(s, "/.env\n") != 1 || !strings.Contains(s, "/wp-config.php\n") || strings.Count(s, "# Hangar") != 1 {
		t.Errorf("unexpected exclude file:\n%s", s)
	}
}
