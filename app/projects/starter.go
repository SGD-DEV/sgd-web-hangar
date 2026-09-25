package projects

import (
	"html"
	"os"
	"path/filepath"
	"strings"
)

// WriteStarterPage puts an index.php into an empty project folder so a new
// site shows something instead of "404 Not Found".
func WriteStarterPage(dir, name, domain string) error {
	target := filepath.Join(dir, "index.php")
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	page := strings.NewReplacer(
		"{{NAME}}", html.EscapeString(name),
		"{{DOMAIN}}", html.EscapeString(domain),
		"{{DIR}}", html.EscapeString(dir),
	).Replace(starterPage)
	return os.WriteFile(target, []byte(page), 0644)
}

func isDirNonEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

const starterPage = `<!DOCTYPE html>
<html lang="de">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{NAME}}</title>
<style>
  body { margin: 0; min-height: 100vh; display: grid; place-items: center;
         font: 16px/1.5 system-ui, sans-serif; background: #0f1010; color: #e8e8e6; }
  main { max-width: 36rem; padding: 2rem; }
  h1 { margin: 0 0 .5rem; font-size: 1.6rem; }
  code { background: #1a1c1a; padding: .1rem .35rem; border-radius: 4px; color: #b5f23d; }
  p { color: #9a9f96; }
</style>
</head>
<body>
<main>
  <h1>{{NAME}} ist online</h1>
  <p>Diese Seite wird von Hangar mit PHP <code><?= PHP_VERSION ?></code> ausgeliefert.</p>
  <p>Ersetze <code>index.php</code> in <code>{{DIR}}</code> durch deine Website.</p>
</main>
</body>
</html>
`
