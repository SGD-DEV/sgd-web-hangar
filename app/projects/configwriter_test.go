package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wpSample = `<?php
define( 'DB_NAME', 'database_name_here' );
define( 'DB_USER', 'username_here' );
define( 'DB_PASSWORD', 'password_here' );
define( 'DB_HOST', 'localhost' );
define( 'AUTH_KEY',         'put your unique phrase here' );
define( 'NONCE_SALT',       'put your unique phrase here' );
$table_prefix = 'wp_';
`

func TestApplyConfigWordPress(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "wp-config-sample.php"), []byte(wpSample), 0644)
	p := Project{Path: dir, Framework: "wordpress",
		Database: &Database{Type: "mysql", Host: "127.0.0.1", Port: 3306, Name: "blog", User: "blog", Password: `pa'ss\`}}
	if _, err := ApplyConfig(p); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, "wp-config.php"))
	for _, want := range []string{
		`define( 'DB_NAME', 'blog' )`,
		`define( 'DB_PASSWORD', 'pa\'ss\\' )`,
		`define( 'DB_HOST', '127.0.0.1' )`,
		`$table_prefix = 'wp_';`,
	} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("wp-config.php lacks %s:\n%s", want, cfg)
		}
	}
	if strings.Contains(string(cfg), "put your unique phrase here") {
		t.Error("salts were not generated")
	}

	// A second run with another DB keeps the salts and updates the values.
	before := string(cfg)
	p.Database.Name, p.Database.Port = "other", 3307
	ApplyConfig(p)
	cfg, _ = os.ReadFile(filepath.Join(dir, "wp-config.php"))
	if !strings.Contains(string(cfg), `define( 'DB_HOST', '127.0.0.1:3307' )`) {
		t.Errorf("DB_HOST not updated:\n%s", cfg)
	}
	saltLine := func(s string) string { return s[strings.Index(s, "'AUTH_KEY'"):][:90] }
	if saltLine(before) != saltLine(string(cfg)) {
		t.Error("salts changed on the second run")
	}

	plugin, err := os.ReadFile(filepath.Join(dir, "wp-content", "mu-plugins", "hangar-smtp.php"))
	if err != nil || !strings.Contains(string(plugin), "$mailer->Port = 1025;") {
		t.Errorf("mail plugin missing or not Mailpit: %v\n%s", err, plugin)
	}
}

func TestApplyConfigWordPressRejectsPostgres(t *testing.T) {
	p := Project{Path: t.TempDir(), Framework: "wordpress", Database: &Database{Type: "postgresql"}}
	if _, err := ApplyConfig(p); err == nil {
		t.Fatal("expected an error for PostgreSQL")
	}
}

func TestApplyConfigDotEnv(t *testing.T) {
	dir := t.TempDir()
	env := "APP_NAME=Laravel\r\nDB_CONNECTION=sqlite\r\n# DB_HOST=127.0.0.1\r\nMAIL_MAILER=log\r\n"
	os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0644)
	p := Project{Path: dir, Framework: "laravel",
		Database: &Database{Type: "postgresql", Host: "127.0.0.1", Port: 5432, Name: "shop", User: "shop", Password: `x"y`},
		Mail:     &Mail{Host: "smtp.example.com", Port: 465, User: "me", Password: "pw", Encryption: "ssl", FromEmail: "shop@example.com"}}
	if _, err := ApplyConfig(p); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, ".env"))
	s := string(out)
	for _, want := range []string{
		"APP_NAME=Laravel\r\n",
		"DB_CONNECTION=\"pgsql\"\r\n",
		"# DB_HOST=127.0.0.1\r\n",
		"DB_PASSWORD=\"x\\\"y\"\r\n",
		"MAIL_MAILER=\"smtp\"\r\n",
		"MAIL_SCHEME=\"smtps\"\r\n",
		"MAIL_PORT=\"465\"\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf(".env lacks %q:\n%s", want, s)
		}
	}
	if strings.Count(s, "MAIL_MAILER=") != 1 {
		t.Errorf("MAIL_MAILER duplicated:\n%s", s)
	}
	if strings.Contains(strings.ReplaceAll(s, "\r\n", ""), "\n") {
		t.Error("mixed line endings")
	}
}

func TestSymfonyURLs(t *testing.T) {
	db := Database{Type: "mysql", Host: "127.0.0.1", Port: 3306, Name: "app", User: "app", Password: "p@ss/word"}
	if got := databaseURL(db); got != "mysql://app:p%40ss%2Fword@127.0.0.1:3306/app" {
		t.Errorf("databaseURL = %s", got)
	}
	if got := mailerDSN(MailpitMail); got != "smtp://127.0.0.1:1025" {
		t.Errorf("mailerDSN = %s", got)
	}
}
