package projects

// configwriter.go: puts a project's database and mail settings where the
// app reads them - wp-config.php plus a must-use plugin for WordPress, .env
// for everything else (Laravel names, which plain PHP can read with
// parse_ini_file). Only Hangar's keys are touched; the rest of the file stays.

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// MailpitMail is the mail setting of a project that has none configured.
var MailpitMail = Mail{Host: "127.0.0.1", Port: 1025}

// SecretFiles are the files ApplyConfig writes credentials into, relative
// to the project folder. They must never end up in a git push.
var SecretFiles = []string{".env", ".env.local", "wp-config.php", "wp-content/mu-plugins/hangar-smtp.php"}

// EffectiveMail returns the project's SMTP settings, Mailpit when unset.
func (p Project) EffectiveMail() Mail {
	if p.Mail != nil {
		return *p.Mail
	}
	return MailpitMail
}

// IsWordPress reports whether the project folder holds a WordPress install.
func (p Project) IsWordPress() bool {
	if p.Framework == string(FrameworkWordPress) {
		return true
	}
	for _, f := range []string{"wp-config.php", "wp-config-sample.php", "wp-load.php"} {
		if _, err := os.Stat(filepath.Join(p.Path, f)); err == nil {
			return true
		}
	}
	return false
}

// ApplyConfig writes the project's database and mail settings into its
// config files and returns the files it changed.
func ApplyConfig(p Project) ([]string, error) {
	if p.Framework == string(FrameworkProxy) || p.Path == "" {
		return nil, nil
	}
	if p.IsWordPress() {
		return applyWordPress(p)
	}
	return applyDotEnv(p)
}

// --- WordPress ---------------------------------------------------------------

func applyWordPress(p Project) ([]string, error) {
	var written []string
	if p.Database != nil {
		if p.Database.Type != "mysql" {
			return nil, fmt.Errorf("WordPress braucht MySQL, nicht %s", p.Database.Type)
		}
		path, err := writeWPConfig(p.Path, *p.Database)
		if err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	path, err := writeWPMailPlugin(p.Path, p.EffectiveMail())
	if err != nil {
		return written, err
	}
	return append(written, path), nil
}

var wpSaltNames = []string{"AUTH_KEY", "SECURE_AUTH_KEY", "LOGGED_IN_KEY", "NONCE_KEY", "AUTH_SALT", "SECURE_AUTH_SALT", "LOGGED_IN_SALT", "NONCE_SALT"}

// writeWPConfig sets the DB_* constants in wp-config.php, creating it from
// wp-config-sample.php (with fresh salts) when it doesn't exist yet.
func writeWPConfig(dir string, db Database) (string, error) {
	target := filepath.Join(dir, "wp-config.php")
	data, err := os.ReadFile(target)
	fresh := false
	if os.IsNotExist(err) {
		data, err = os.ReadFile(filepath.Join(dir, "wp-config-sample.php"))
		if err != nil {
			return "", fmt.Errorf("weder wp-config.php noch wp-config-sample.php in %s gefunden", dir)
		}
		fresh = true
	} else if err != nil {
		return "", err
	}
	host := db.Host
	if db.Port != 0 && db.Port != 3306 {
		host = fmt.Sprintf("%s:%d", db.Host, db.Port)
	}
	values := map[string]string{"DB_NAME": db.Name, "DB_USER": db.User, "DB_PASSWORD": db.Password, "DB_HOST": host}
	if fresh {
		for _, k := range wpSaltNames {
			values[k] = randomSalt(64)
		}
	}
	out := string(data)
	for k, v := range values {
		out = setPHPDefine(out, k, v)
	}
	return target, os.WriteFile(target, []byte(out), 0644)
}

// setPHPDefine replaces the value of define('NAME', '...') in PHP source.
func setPHPDefine(src, name, value string) string {
	re := regexp.MustCompile(`define\(\s*['"]` + regexp.QuoteMeta(name) + `['"]\s*,\s*'(?:[^'\\]|\\.)*'\s*\)`)
	return re.ReplaceAllLiteralString(src, fmt.Sprintf("define( '%s', %s )", name, phpString(value)))
}

func writeWPMailPlugin(dir string, m Mail) (string, error) {
	plugins := filepath.Join(dir, "wp-content", "mu-plugins")
	if err := os.MkdirAll(plugins, 0755); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("<?php\n/*\n * Plugin Name: Hangar SMTP\n * Description: Mail settings from Hangar (project > Mail). This file is rewritten by Hangar - change the settings there.\n */\n\n")
	b.WriteString("add_action( 'phpmailer_init', function ( $mailer ) {\n")
	b.WriteString("\t$mailer->isSMTP();\n")
	fmt.Fprintf(&b, "\t$mailer->Host = %s;\n", phpString(m.Host))
	fmt.Fprintf(&b, "\t$mailer->Port = %d;\n", m.Port)
	fmt.Fprintf(&b, "\t$mailer->SMTPAuth = %t;\n", m.User != "")
	fmt.Fprintf(&b, "\t$mailer->Username = %s;\n", phpString(m.User))
	fmt.Fprintf(&b, "\t$mailer->Password = %s;\n", phpString(m.Password))
	fmt.Fprintf(&b, "\t$mailer->SMTPSecure = %s;\n", phpString(m.Encryption))
	fmt.Fprintf(&b, "\t$mailer->SMTPAutoTLS = %t;\n", m.Encryption != "")
	b.WriteString("} );\n")
	if m.FromEmail != "" {
		fmt.Fprintf(&b, "\nadd_filter( 'wp_mail_from', function () { return %s; } );\n", phpString(m.FromEmail))
	}
	if m.FromName != "" {
		fmt.Fprintf(&b, "add_filter( 'wp_mail_from_name', function () { return %s; } );\n", phpString(m.FromName))
	}
	target := filepath.Join(plugins, "hangar-smtp.php")
	return target, os.WriteFile(target, []byte(b.String()), 0644)
}

// phpString quotes s as a single-quoted PHP string literal.
func phpString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

const saltChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#%^&*()-_[]{}<>~+=,.;:/?|"

func randomSalt(n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(saltChars)))
	for i := range b {
		x, _ := rand.Int(rand.Reader, max)
		b[i] = saltChars[x.Int64()]
	}
	return string(b)
}

// --- .env --------------------------------------------------------------------

type envPair struct{ key, value string }

func applyDotEnv(p Project) ([]string, error) {
	var pairs []envPair
	if db := p.Database; db != nil {
		conn := "mysql"
		if db.Type == "postgresql" {
			conn = "pgsql"
		}
		pairs = append(pairs,
			envPair{"DB_CONNECTION", conn},
			envPair{"DB_HOST", db.Host},
			envPair{"DB_PORT", strconv.Itoa(db.Port)},
			envPair{"DB_DATABASE", db.Name},
			envPair{"DB_USERNAME", db.User},
			envPair{"DB_PASSWORD", db.Password},
		)
	}
	m := p.EffectiveMail()
	scheme := "smtp"
	if m.Encryption == "ssl" {
		scheme = "smtps"
	}
	pairs = append(pairs,
		envPair{"MAIL_MAILER", "smtp"},
		envPair{"MAIL_SCHEME", scheme},
		envPair{"MAIL_HOST", m.Host},
		envPair{"MAIL_PORT", strconv.Itoa(m.Port)},
		envPair{"MAIL_USERNAME", m.User},
		envPair{"MAIL_PASSWORD", m.Password},
		envPair{"MAIL_ENCRYPTION", m.Encryption},
		envPair{"MAIL_FROM_ADDRESS", m.FromEmail},
		envPair{"MAIL_FROM_NAME", m.FromName},
	)

	written := []string{}
	path, err := updateEnvFile(filepath.Join(p.Path, ".env"), pairs)
	if err != nil {
		return nil, err
	}
	written = append(written, path)

	if p.Framework == string(FrameworkSymfony) {
		var sym []envPair
		if db := p.Database; db != nil {
			sym = append(sym, envPair{"DATABASE_URL", databaseURL(*db)})
		}
		sym = append(sym, envPair{"MAILER_DSN", mailerDSN(m)})
		path, err := updateEnvFile(filepath.Join(p.Path, ".env.local"), sym)
		if err != nil {
			return written, err
		}
		written = append(written, path)
	}
	return written, nil
}

// updateEnvFile sets each key in a dotenv file, replacing an existing
// KEY=... line or appending it, and keeps the file's line endings.
func updateEnvFile(path string, pairs []envPair) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	content := string(data)
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	var missing []string
	for _, kv := range pairs {
		line := kv.key + "=" + envQuote(kv.value)
		re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(kv.key) + `[ \t]*=[^\r\n]*`)
		if re.MatchString(content) {
			content = re.ReplaceAllLiteralString(content, line)
		} else {
			missing = append(missing, line)
		}
	}
	if len(missing) > 0 {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += nl
		}
		if content != "" {
			content += nl
		}
		content += "# Written by Hangar (project > Database / Mail)" + nl + strings.Join(missing, nl) + nl
	}
	return path, os.WriteFile(path, []byte(content), 0644)
}

// envQuote double-quotes a dotenv value. Both Laravel's phpdotenv and PHP's
// parse_ini_file read this form.
func envQuote(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

func databaseURL(db Database) string {
	scheme := "mysql"
	if db.Type == "postgresql" {
		scheme = "postgresql"
	}
	u := url.URL{Scheme: scheme, User: url.UserPassword(db.User, db.Password), Host: fmt.Sprintf("%s:%d", db.Host, db.Port), Path: "/" + db.Name}
	return u.String()
}

func mailerDSN(m Mail) string {
	scheme := "smtp"
	if m.Encryption == "ssl" {
		scheme = "smtps"
	}
	u := url.URL{Scheme: scheme, Host: fmt.Sprintf("%s:%d", m.Host, m.Port)}
	if m.User != "" {
		u.User = url.UserPassword(m.User, m.Password)
	}
	return u.String()
}
