package app

// php_ini_defaults.go: the settings Hangar applies to every php.ini - mail
// to Mailpit, OPcache, a CA bundle for outgoing HTTPS. Only untouched stock
// lines are rewritten, so values a user changed survive.

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type iniRewrite struct {
	re   *regexp.Regexp
	repl string
}

// stockLine matches one exact line of php.ini-production (any trailing \r
// is kept via $1).
func stockLine(line string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(line) + `[ \t]*(\r?)$`)
}

var staticINIRewrites = []iniRewrite{
	// PHP's mail() on Windows talks SMTP directly to SMTP:smtp_port. The
	// stock php.ini points it at localhost:25 where nothing listens, so
	// mails from contact forms were silently lost. 127.0.0.1, not
	// localhost: Mailpit listens on IPv4 only and localhost may resolve to
	// ::1 first. Without a sender mail() fails unless the script sets one.
	{regexp.MustCompile(`(?m)^[ \t]*SMTP[ \t]*=[ \t]*localhost[ \t]*(\r?)$`), "SMTP = 127.0.0.1$1"},
	{regexp.MustCompile(`(?m)^[ \t]*smtp_port[ \t]*=[ \t]*25[ \t]*(\r?)$`), "smtp_port = 1025$1"},
	{regexp.MustCompile(`(?m)^;[ \t]*sendmail_from[ \t]*=[ \t]*me@example\.com[ \t]*(\r?)$`), "sendmail_from = hangar@localhost$1"},

	// OPcache is off in the stock Windows php.ini: every request compiles
	// all PHP files again (WordPress: ~0.25 s instead of ~0.05 s per
	// request). Timestamps stay validated so edited files show up within
	// two seconds.
	{stockLine(";zend_extension=opcache"), "zend_extension=opcache$1"},
	{stockLine(";opcache.enable=1"), "opcache.enable=1$1"},
	{stockLine(";opcache.memory_consumption=128"), "opcache.memory_consumption=192$1"},
	{stockLine(";opcache.interned_strings_buffer=8"), "opcache.interned_strings_buffer=16$1"},
	{stockLine(";opcache.max_accelerated_files=10000"), "opcache.max_accelerated_files=20000$1"},
	{stockLine(";opcache.validate_timestamps=1"), "opcache.validate_timestamps=1$1"},
	{stockLine(";opcache.revalidate_freq=2"), "opcache.revalidate_freq=2$1"},
	// OPcache is a Zend extension; an extension=opcache line (written by an
	// older extension toggle) only produces a startup warning.
	{regexp.MustCompile(`(?m)^extension=(php_)?opcache(\.dll)?[ \t]*(\r?)$`), ";extension=opcache ; wrong directive, loaded via zend_extension$3"},
	// Resolved include paths are cached per worker; keep them longer.
	{stockLine(";realpath_cache_ttl = 120"), "realpath_cache_ttl = 600$1"},
}

// hangarINIDefaults returns data with Hangar's defaults applied and reports
// whether anything changed. caBundle may be empty (then the CA lines stay).
func hangarINIDefaults(data []byte, caBundle string) ([]byte, bool) {
	rewrites := staticINIRewrites
	if caBundle != "" {
		q := `"` + caBundle + `"`
		rewrites = append(append([]iniRewrite{}, rewrites...),
			iniRewrite{regexp.MustCompile(`(?m)^;curl\.cainfo[ \t]*=[ \t]*(\r?)$`), "curl.cainfo = " + escapeRepl(q) + "$1"},
			iniRewrite{regexp.MustCompile(`(?m)^;openssl\.cafile[ \t]*=[ \t]*(\r?)$`), "openssl.cafile = " + escapeRepl(q) + "$1"},
		)
	}
	out := data
	for _, r := range rewrites {
		out = r.re.ReplaceAll(out, []byte(r.repl))
	}
	return out, string(out) != string(data)
}

// escapeRepl protects $ in a regexp replacement string.
func escapeRepl(s string) string { return strings.ReplaceAll(s, "$", "$$") }

// applyHangarINIDefaults rewrites an existing php.ini in place and reports
// whether it changed (the PHP workers then need a restart).
func applyHangarINIDefaults(iniPath, caBundle string) (bool, error) {
	data, err := os.ReadFile(iniPath)
	if err != nil {
		return false, err
	}
	patched, changed := hangarINIDefaults(data, caBundle)
	if !changed {
		return false, nil
	}
	return true, os.WriteFile(iniPath, patched, 0644)
}

const mozillaCABundleURL = "https://curl.se/ca/cacert.pem"

// ensureCABundle keeps <data>/ssl/hangar-ca-bundle.pem: Mozilla's CA list
// (for curl/openssl in PHP, which on Windows has none) plus Hangar's local
// mkcert root, so PHP also trusts the https://*.test sites on this machine
// (WordPress loopback requests, Site Health). Refreshed monthly. Returns ""
// when no bundle could be built.
func (a *App) ensureCABundle() string {
	target := filepath.Join(a.paths.SSLPath(), "hangar-ca-bundle.pem")
	if info, err := os.Stat(target); err == nil && time.Since(info.ModTime()) < 30*24*time.Hour {
		return target
	}
	mozilla, err := fetchText(mozillaCABundleURL)
	if err != nil {
		// Offline: fall back to the bundle Git for Windows ships.
		data, gitErr := os.ReadFile(`C:\Program Files\Git\mingw64\etc\ssl\certs\ca-bundle.crt`)
		if gitErr != nil {
			if _, statErr := os.Stat(target); statErr == nil {
				return target // keep the old one
			}
			return ""
		}
		mozilla = string(data)
	}
	bundle := strings.TrimRight(mozilla, "\n") + "\n"
	if a.sslManager != nil {
		if root, err := os.ReadFile(filepath.Join(a.sslManager.CARoot(), "rootCA.pem")); err == nil {
			bundle += "\nHangar local development CA (mkcert)\n====================================\n" + string(root)
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return ""
	}
	if err := os.WriteFile(target, []byte(bundle), 0644); err != nil {
		return ""
	}
	return target
}

func fetchText(url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second} // runs during startup
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &os.PathError{Op: "GET", Path: url, Err: os.ErrNotExist}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", err
	}
	if !strings.Contains(string(data), "BEGIN CERTIFICATE") {
		return "", &os.PathError{Op: "parse", Path: url, Err: os.ErrInvalid}
	}
	return string(data), nil
}
