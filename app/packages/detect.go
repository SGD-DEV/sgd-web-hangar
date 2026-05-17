package packages

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

// DetectedPackage is what URLDetect returns: a best-effort guess of what kind
// of package a URL represents and which version it is, plus a human label
// suggestion. Caller can prefill an "Add custom package" form with this and
// let the user correct anything that's wrong.
type DetectedPackage struct {
	Name       string `json:"name"`        // canonical name: php / mysql / nginx / ...
	Version    string `json:"version"`     // parsed version, e.g. "8.4.13" or "12.8"
	Label      string `json:"label"`       // human-readable, e.g. "PHP 8.4.13"
	Category   string `json:"category"`    // category id, matches CategoryX constants
	SubDir     string `json:"sub_dir"`     // suggested install subdir, e.g. "php/8.4"
	Confidence string `json:"confidence"`  // "high" | "medium" | "low" | "unknown"
	Note       string `json:"note,omitempty"`
}

// hostMatcher associates a hostname pattern with a recognizer function.
// First match wins. Patterns can be exact hosts or substrings.
type hostMatcher struct {
	hostContains string
	pathContains string // optional - additional disambiguator
	detect       func(u *url.URL) DetectedPackage
}

var matchers = []hostMatcher{
	{hostContains: "windows.php.net", detect: detectPHP},
	{hostContains: "dev.mysql.com", detect: detectMySQL},
	{hostContains: "enterprisedb.com", detect: detectPostgres},
	{hostContains: "nginx.org", detect: detectNginx},
	{hostContains: "apachelounge.com", detect: detectApache},
	{hostContains: "nodejs.org", detect: detectNode},
	{hostContains: "getcomposer.org", detect: detectComposer},
	{hostContains: "go.dev", detect: detectGo},
	{hostContains: "files.phpmyadmin.net", detect: detectPhpMyAdmin},
	{hostContains: "heidisql.com", detect: detectHeidiSQL},
	// GitHub releases catch-all - branch on repo name
	{hostContains: "github.com", pathContains: "/releases/", detect: detectGitHubRelease},
}

// URLDetect inspects rawURL and returns a best-guess DetectedPackage. When
// the URL doesn't match any known host, returns a low-confidence fallback
// with Name="custom" and the filename used as a version hint.
func URLDetect(rawURL string) DetectedPackage {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return unknownPackage(rawURL)
	}
	host := strings.ToLower(u.Host)
	urlPath := u.Path

	for _, m := range matchers {
		if !strings.Contains(host, m.hostContains) {
			continue
		}
		if m.pathContains != "" && !strings.Contains(urlPath, m.pathContains) {
			continue
		}
		return m.detect(u)
	}
	return unknownPackage(rawURL)
}

// unknownPackage is the fallback when nothing in `matchers` recognizes the
// URL. We still return *something* so the user can type in a name/version
// manually and proceed.
func unknownPackage(rawURL string) DetectedPackage {
	u, _ := url.Parse(rawURL)
	filename := ""
	if u != nil {
		filename = path.Base(u.Path)
	}
	return DetectedPackage{
		Name:       "custom",
		Version:    "",
		Label:      "Custom: " + filename,
		Category:   string(CategoryTools),
		SubDir:     "custom/" + safeName(filename),
		Confidence: "unknown",
		Note:       "Couldn't recognize this host - fill in name/version manually.",
	}
}

// --- per-host detectors ---

// detectPHP: https://windows.php.net/downloads/releases/archives/php-8.4.12-nts-Win32-vs17-x64.zip
var phpVersionRE = regexp.MustCompile(`php-(\d+\.\d+\.\d+)`)

func detectPHP(u *url.URL) DetectedPackage {
	filename := path.Base(u.Path)
	major := ""
	full := ""
	if m := phpVersionRE.FindStringSubmatch(filename); m != nil {
		full = m[1]
		// Take just major.minor for SubDir (matches existing registry
		// convention "php/8.3" not "php/8.3.25").
		parts := strings.SplitN(full, ".", 3)
		if len(parts) >= 2 {
			major = parts[0] + "." + parts[1]
		}
	}
	if major == "" {
		major = "unknown"
	}
	displayVer := full
	if displayVer == "" {
		displayVer = major
	}
	return DetectedPackage{
		Name:       "php",
		Version:    major,
		Label:      "PHP " + displayVer,
		Category:   string(CategoryPHP),
		SubDir:     "php/" + major,
		Confidence: "high",
	}
}

// detectMySQL: https://dev.mysql.com/get/Downloads/MySQL-9.4/mysql-9.4.0-winx64.zip
var mysqlVersionRE = regexp.MustCompile(`mysql-(\d+\.\d+(?:\.\d+)?)-`)

func detectMySQL(u *url.URL) DetectedPackage {
	full := ""
	if m := mysqlVersionRE.FindStringSubmatch(u.Path); m != nil {
		full = m[1]
	}
	short := full
	parts := strings.SplitN(full, ".", 3)
	if len(parts) >= 2 {
		short = parts[0] + "." + parts[1]
	}
	if short == "" {
		short = "unknown"
	}
	return DetectedPackage{
		Name:       "mysql",
		Version:    short,
		Label:      "MySQL " + full,
		Category:   string(CategoryDatabase),
		SubDir:     "mysql/" + short,
		Confidence: "high",
	}
}

// detectPostgres: get.enterprisedb.com/postgresql/postgresql-18.3-1-windows-x64-binaries.zip
//                 sbp.enterprisedb.com/getfile.jsp?fileid=... (older, less reliable)
var postgresVersionRE = regexp.MustCompile(`postgresql-(\d+\.\d+(?:\.\d+)?)`)

func detectPostgres(u *url.URL) DetectedPackage {
	full := ""
	if m := postgresVersionRE.FindStringSubmatch(u.Path); m != nil {
		full = m[1]
	}
	if full == "" {
		// sbp URLs don't expose version - admit it.
		return DetectedPackage{
			Name:       "postgresql",
			Version:    "",
			Label:      "PostgreSQL (version unknown)",
			Category:   string(CategoryDatabase),
			SubDir:     "postgresql/custom",
			Confidence: "low",
			Note:       "EnterpriseDB getfile.jsp URLs don't include the version. Type one in.",
		}
	}
	return DetectedPackage{
		Name:       "postgresql",
		Version:    full,
		Label:      "PostgreSQL " + full,
		Category:   string(CategoryDatabase),
		SubDir:     "postgresql/" + full,
		Confidence: "high",
	}
}

// detectNginx: nginx.org/download/nginx-1.29.1.zip
var nginxVersionRE = regexp.MustCompile(`nginx-(\d+\.\d+\.\d+)`)

func detectNginx(u *url.URL) DetectedPackage {
	full := ""
	if m := nginxVersionRE.FindStringSubmatch(u.Path); m != nil {
		full = m[1]
	}
	if full == "" {
		full = "unknown"
	}
	return DetectedPackage{
		Name:       "nginx",
		Version:    full,
		Label:      "Nginx " + full,
		Category:   string(CategoryWebServer),
		SubDir:     "nginx/" + full,
		Confidence: "high",
	}
}

// detectApache: apachelounge.com/download/VS17/binaries/httpd-2.4.65-250724-Win64-VS17.zip
var apacheVersionRE = regexp.MustCompile(`httpd-(\d+\.\d+\.\d+)`)

func detectApache(u *url.URL) DetectedPackage {
	full := ""
	if m := apacheVersionRE.FindStringSubmatch(u.Path); m != nil {
		full = m[1]
	}
	if full == "" {
		full = "unknown"
	}
	return DetectedPackage{
		Name:       "apache",
		Version:    full,
		Label:      "Apache " + full,
		Category:   string(CategoryWebServer),
		SubDir:     "apache/" + full,
		Confidence: "high",
	}
}

// detectNode: nodejs.org/dist/v24.9.0/node-v24.9.0-win-x64.zip
var nodeVersionRE = regexp.MustCompile(`node-v(\d+\.\d+\.\d+)`)

func detectNode(u *url.URL) DetectedPackage {
	full := ""
	if m := nodeVersionRE.FindStringSubmatch(u.Path); m != nil {
		full = m[1]
	}
	short := full
	parts := strings.SplitN(full, ".", 3)
	if len(parts) >= 2 {
		short = parts[0] + "." + parts[1]
	}
	if short == "" {
		short = "unknown"
	}
	return DetectedPackage{
		Name:       "node",
		Version:    short,
		Label:      "Node.js " + full,
		Category:   string(CategoryNodeJS),
		SubDir:     "node/" + short,
		Confidence: "high",
	}
}

// detectComposer: getcomposer.org/download/2.8.4/composer.phar
var composerVersionRE = regexp.MustCompile(`/download/(\d+\.\d+\.\d+)/`)

func detectComposer(u *url.URL) DetectedPackage {
	full := ""
	if m := composerVersionRE.FindStringSubmatch(u.Path); m != nil {
		full = m[1]
	}
	if full == "" {
		full = "latest"
	}
	return DetectedPackage{
		Name:       "composer",
		Version:    full,
		Label:      "Composer " + full,
		Category:   string(CategoryTools),
		SubDir:     "composer", // intentional flat layout (see registry.go comment)
		Confidence: "high",
	}
}

// detectGo: go.dev/dl/go1.24.1.windows-amd64.zip
var goVersionRE = regexp.MustCompile(`go(\d+\.\d+(?:\.\d+)?)\.windows`)

func detectGo(u *url.URL) DetectedPackage {
	full := ""
	if m := goVersionRE.FindStringSubmatch(u.Path); m != nil {
		full = m[1]
	}
	short := full
	parts := strings.SplitN(full, ".", 3)
	if len(parts) >= 2 {
		short = parts[0] + "." + parts[1]
	}
	if short == "" {
		short = "unknown"
	}
	return DetectedPackage{
		Name:       "go",
		Version:    short,
		Label:      "Go " + full,
		Category:   string(CategoryGolang),
		SubDir:     "go/" + short,
		Confidence: "high",
	}
}

// detectPhpMyAdmin: files.phpmyadmin.net/phpMyAdmin/5.2.2/phpMyAdmin-5.2.2-english.zip
var phpMyAdminVersionRE = regexp.MustCompile(`phpMyAdmin/(\S+?)/`)

func detectPhpMyAdmin(u *url.URL) DetectedPackage {
	ver := ""
	if m := phpMyAdminVersionRE.FindStringSubmatch(u.Path); m != nil {
		ver = m[1]
	}
	if ver == "" {
		ver = "unknown"
	}
	return DetectedPackage{
		Name:       "phpmyadmin",
		Version:    ver,
		Label:      "phpMyAdmin " + ver,
		Category:   string(CategoryTools),
		SubDir:     "phpmyadmin/" + ver,
		Confidence: "high",
	}
}

// detectHeidiSQL: heidisql.com/downloads/releases/HeidiSQL_12.8_64_Portable.zip
var heidiVersionRE = regexp.MustCompile(`HeidiSQL_(\d+\.\d+)`)

func detectHeidiSQL(u *url.URL) DetectedPackage {
	ver := ""
	if m := heidiVersionRE.FindStringSubmatch(u.Path); m != nil {
		ver = m[1]
	}
	if ver == "" {
		ver = "unknown"
	}
	return DetectedPackage{
		Name:       "heidisql",
		Version:    ver,
		Label:      "HeidiSQL " + ver,
		Category:   string(CategoryTools),
		SubDir:     "heidisql/" + ver,
		Confidence: "high",
	}
}

// detectGitHubRelease: github.com/<owner>/<repo>/releases/download/<tag>/<file>
// We branch on the owner/repo to identify known projects (mailpit, pocketbase,
// etc.). For unknown repos we fall through to a generic "tools" entry with
// the repo name as the package name.
var githubReleaseRE = regexp.MustCompile(`^/([^/]+)/([^/]+)/releases/download/v?([^/]+)/`)

func detectGitHubRelease(u *url.URL) DetectedPackage {
	m := githubReleaseRE.FindStringSubmatch(u.Path)
	if m == nil {
		return unknownPackage(u.String())
	}
	repo := strings.ToLower(m[2])
	tag := m[3]

	// Strip a leading "v" from the tag if a version-style tag was used
	// (mailpit uses "v1.24.2", pocketbase uses "v0.25.9").
	tag = strings.TrimPrefix(tag, "v")

	switch {
	case repo == "mailpit" || strings.Contains(repo, "mailpit"):
		short := majorMinor(tag)
		return DetectedPackage{
			Name: "mailpit", Version: short, Label: "Mailpit " + tag,
			Category: string(CategoryTools), SubDir: "mailpit/" + short,
			Confidence: "high",
		}
	case repo == "pocketbase":
		return DetectedPackage{
			Name: "pocketbase", Version: tag, Label: "PocketBase " + tag,
			Category: string(CategoryTools), SubDir: "pocketbase/" + tag,
			Confidence: "high",
		}
	case repo == "heidisql":
		// HeidiSQL release URLs live at github.com/HeidiSQL/HeidiSQL/
		// releases/download/<tag>/... - tag is just "12.17" with no
		// leading v. Use the tag directly as version + install subdir.
		return DetectedPackage{
			Name: "heidisql", Version: tag, Label: "HeidiSQL " + tag,
			Category: string(CategoryTools), SubDir: "heidisql/" + tag,
			Confidence: "high",
		}
	default:
		// Unknown GitHub project. Use the repo name as a sensible default.
		return DetectedPackage{
			Name: repo, Version: tag, Label: m[2] + " " + tag,
			Category: string(CategoryTools), SubDir: repo + "/" + tag,
			Confidence: "medium",
			Note:       "Unknown GitHub project - using repo name. Edit if needed.",
		}
	}
}

// --- helpers ---

func majorMinor(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

// safeName strips characters that are illegal in Windows directory names.
func safeName(s string) string {
	for _, ch := range []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|", " "} {
		s = strings.ReplaceAll(s, ch, "_")
	}
	if s == "" {
		s = "unknown"
	}
	return s
}
