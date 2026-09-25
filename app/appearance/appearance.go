// Package appearance stores the look of Hangar itself (name, accent colour,
// logo) and the template of the start page new projects get.
//
// Everything lives in <data>/appearance: settings.json plus the logo file.
package appearance

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// MaxLogoBytes keeps the logo small enough to inline into start pages.
const MaxLogoBytes = 512 * 1024

// Starter is the template for the index.php of new projects.
type Starter struct {
	Lang       string `json:"lang"`    // "de" | "en"
	Heading    string `json:"heading"` // placeholders: {name} {domain} {php} {folder}
	Text       string `json:"text"`
	Background string `json:"background"`
	Accent     string `json:"accent"` // empty = the app accent
	ShowLogo   bool   `json:"show_logo"`
	ShowPHP    bool   `json:"show_php"`
}

// Settings is the whole Appearance page.
type Settings struct {
	AppName string  `json:"app_name"`
	Accent  string  `json:"accent"`
	Logo    string  `json:"logo"` // data: URL, empty = none (not stored in the JSON)
	Starter Starter `json:"starter"`
}

// StarterTexts are the default heading/text per language.
var StarterTexts = map[string][2]string{
	"de": {"{name} ist online", "Diese Seite wird von Hangar mit PHP {php} ausgeliefert. Ersetze index.php in {folder} durch deine Website."},
	"en": {"{name} is live", "This page is served by Hangar with PHP {php}. Replace index.php in {folder} with your site."},
}

// Defaults are the settings before anything was saved.
func Defaults() Settings {
	t := StarterTexts["de"]
	return Settings{
		AppName: "Hangar",
		Accent:  "#b5f23d",
		Starter: Starter{Lang: "de", Heading: t[0], Text: t[1], Background: "#0f1010", ShowLogo: true, ShowPHP: true},
	}
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Store reads and writes the settings in dir.
type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) settingsPath() string { return filepath.Join(s.dir, "settings.json") }

func (s *Store) logoPath() (string, error) {
	matches, _ := filepath.Glob(filepath.Join(s.dir, "logo.*"))
	if len(matches) == 0 {
		return "", os.ErrNotExist
	}
	return matches[0], nil
}

// Load returns the saved settings merged over the defaults, with the logo
// as a data URL.
func (s *Store) Load() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Defaults()
	if data, err := os.ReadFile(s.settingsPath()); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	st.Logo = ""
	if p, err := s.logoPath(); err == nil {
		if data, err := os.ReadFile(p); err == nil {
			st.Logo = "data:" + logoMime(p) + ";base64," + base64.StdEncoding.EncodeToString(data)
		}
	}
	return st
}

// Save validates and stores the settings (the logo is set separately).
func (s *Store) Save(st Settings) (Settings, error) {
	st.AppName = strings.TrimSpace(st.AppName)
	if st.AppName == "" {
		st.AppName = "Hangar"
	}
	if len([]rune(st.AppName)) > 40 {
		return st, fmt.Errorf("the name may have at most 40 characters")
	}
	if !hexColor.MatchString(st.Accent) {
		return st, fmt.Errorf("accent colour must look like #b5f23d")
	}
	if !hexColor.MatchString(st.Starter.Background) {
		return st, fmt.Errorf("background colour must look like #0f1010")
	}
	if st.Starter.Accent != "" && !hexColor.MatchString(st.Starter.Accent) {
		return st, fmt.Errorf("start page accent must look like #b5f23d or be empty")
	}
	if st.Starter.Lang != "de" && st.Starter.Lang != "en" {
		st.Starter.Lang = "de"
	}
	st.Logo = ""
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return st, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return st, err
	}
	if err := os.WriteFile(s.settingsPath(), data, 0644); err != nil {
		return st, err
	}
	return st, nil
}

var logoExts = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".svg": "image/svg+xml", ".webp": "image/webp"}

func logoMime(path string) string {
	if m, ok := logoExts[strings.ToLower(filepath.Ext(path))]; ok {
		return m
	}
	return mime.TypeByExtension(filepath.Ext(path))
}

// SetLogo copies an image file in as the logo, replacing the old one.
func (s *Store) SetLogo(src string) error {
	ext := strings.ToLower(filepath.Ext(src))
	if _, ok := logoExts[ext]; !ok {
		return fmt.Errorf("logo must be a PNG, JPG, SVG or WebP file")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if len(data) > MaxLogoBytes {
		return fmt.Errorf("logo is %d KB - at most %d KB, please use a smaller image", len(data)/1024, MaxLogoBytes/1024)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	if old, err := s.logoPath(); err == nil {
		os.Remove(old)
	}
	return os.WriteFile(filepath.Join(s.dir, "logo"+ext), data, 0644)
}

// RemoveLogo deletes the logo.
func (s *Store) RemoveLogo() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, err := s.logoPath(); err == nil {
		return os.Remove(p)
	}
	return nil
}

// RenderStarter builds the start page for a project. phpVersion is written
// as PHP code (<?= PHP_VERSION ?>) when empty, so the page shows the
// version it actually runs on; the preview passes a fixed one.
func RenderStarter(st Settings, name, domain, dir, phpVersion string) string {
	s := st.Starter
	accent := s.Accent
	if accent == "" {
		accent = st.Accent
	}
	php := "<code><?= PHP_VERSION ?></code>"
	if phpVersion != "" {
		php = "<code>" + html.EscapeString(phpVersion) + "</code>"
	}
	fill := func(t string) string {
		t = html.EscapeString(t)
		r := strings.NewReplacer(
			"{name}", html.EscapeString(name),
			"{domain}", html.EscapeString(domain),
			"{folder}", "<code>"+html.EscapeString(dir)+"</code>",
			"{php}", php,
		)
		return r.Replace(t)
	}
	text := s.Text
	if !s.ShowPHP {
		// Drop the sentence that mentions the PHP version.
		var keep []string
		for _, sentence := range splitSentences(text) {
			if !strings.Contains(sentence, "{php}") {
				keep = append(keep, sentence)
			}
		}
		text = strings.Join(keep, " ")
	}
	logo := ""
	if s.ShowLogo && st.Logo != "" {
		logo = `<img class="logo" src="` + html.EscapeString(st.Logo) + `" alt="">` + "\n  "
	}
	lang := s.Lang
	if lang == "" {
		lang = "de"
	}
	fg := textOn(s.Background)
	muted := mixHex(s.Background, fg, 0.6)
	codeBg := mixHex(s.Background, fg, 0.08)
	return `<!DOCTYPE html>
<html lang="` + lang + `">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(name) + `</title>
<style>
  body { margin: 0; min-height: 100vh; display: grid; place-items: center;
         font: 16px/1.5 system-ui, sans-serif; background: ` + s.Background + `; color: ` + fg + `; }
  main { max-width: 36rem; padding: 2rem; }
  .logo { max-height: 56px; max-width: 220px; margin-bottom: 1.25rem; display: block; }
  h1 { margin: 0 0 .5rem; font-size: 1.6rem; }
  h1::after { content: ""; display: block; width: 3rem; height: 3px; margin-top: .6rem; background: ` + accent + `; border-radius: 2px; }
  code { background: ` + codeBg + `; padding: .1rem .35rem; border-radius: 4px; color: ` + accent + `; }
  p { color: ` + muted + `; }
</style>
</head>
<body>
<main>
  ` + logo + `<h1>` + fill(s.Heading) + `</h1>
  <p>` + fill(text) + `</p>
</main>
</body>
</html>
`
}

func splitSentences(t string) []string {
	var out []string
	start := 0
	for i := 0; i < len(t); i++ {
		if t[i] == '.' && (i+1 == len(t) || t[i+1] == ' ') {
			out = append(out, strings.TrimSpace(t[start:i+1]))
			start = i + 1
		}
	}
	if rest := strings.TrimSpace(t[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

func parseHex(h string) (r, g, b float64) {
	var ri, gi, bi int
	fmt.Sscanf(strings.TrimPrefix(h, "#"), "%02x%02x%02x", &ri, &gi, &bi)
	return float64(ri), float64(gi), float64(bi)
}

// mixHex blends a toward b by t (0..1).
func mixHex(a, b string, t float64) string {
	ar, ag, ab := parseHex(a)
	br, bg, bb := parseHex(b)
	return fmt.Sprintf("#%02x%02x%02x", int(ar+(br-ar)*t), int(ag+(bg-ag)*t), int(ab+(bb-ab)*t))
}

// textOn picks a readable text colour for a background.
func textOn(bg string) string {
	r, g, b := parseHex(bg)
	if 0.2126*r+0.7152*g+0.0722*b > 150 {
		return "#16181a"
	}
	return "#e8e8e6"
}
