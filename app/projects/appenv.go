package projects

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// AppEnvironment is the environment an app project's service gets: PORT,
// the project's database and mail settings (same names as in .env), and
// the user's own KEY=VALUE lines, which win over the generated ones.
func AppEnvironment(p Project) []string {
	env := map[string]string{
		"HOST":             "127.0.0.1",
		"PYTHONUNBUFFERED": "1", // print() shows up in the log right away
		"PYTHONUTF8":       "1", // UTF-8 log output instead of the ANSI code page
	}
	if p.App != nil {
		env["PORT"] = strconv.Itoa(p.App.Port)
	}
	if db := p.Database; db != nil {
		conn := "mysql"
		if db.Type == "postgresql" {
			conn = "pgsql"
		}
		env["DB_CONNECTION"] = conn
		env["DB_HOST"] = db.Host
		env["DB_PORT"] = strconv.Itoa(db.Port)
		env["DB_DATABASE"] = db.Name
		env["DB_USERNAME"] = db.User
		env["DB_PASSWORD"] = db.Password
		env["DATABASE_URL"] = databaseURL(*db)
	}
	m := p.EffectiveMail()
	env["MAIL_HOST"] = m.Host
	env["MAIL_PORT"] = strconv.Itoa(m.Port)
	env["MAIL_USERNAME"] = m.User
	env["MAIL_PASSWORD"] = m.Password
	env["MAIL_ENCRYPTION"] = m.Encryption
	env["MAIL_FROM_ADDRESS"] = m.FromEmail
	env["MAIL_FROM_NAME"] = m.FromName
	env["MAILER_DSN"] = mailerDSN(m)
	if p.App != nil {
		for _, line := range p.App.Env {
			if k, v, ok := strings.Cut(line, "="); ok {
				env[strings.TrimSpace(k)] = v
			}
		}
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		// NSSM drops the whole AppEnvironmentExtra list when one entry
		// has an empty value, so the app would get no PORT at all.
		if v == "" {
			continue
		}
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// ValidateApp checks an app configuration from the App dialog.
func ValidateApp(a *AppService) error {
	a.Command = strings.TrimSpace(a.Command)
	a.BuildCommand = strings.TrimSpace(a.BuildCommand)
	if a.Command == "" {
		return fmt.Errorf("a start command is required, e.g. npm start")
	}
	if strings.ContainsAny(a.Command+a.BuildCommand, "\r\n") {
		return fmt.Errorf("commands must be a single line - chain steps with &&")
	}
	if a.Port < 1024 || a.Port > 65535 {
		return fmt.Errorf("port must be between 1024 and 65535")
	}
	var env []string
	for _, line := range a.Env {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, _, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			return fmt.Errorf("environment line %q must look like KEY=value", line)
		}
		env = append(env, line)
	}
	a.Env = env
	return nil
}
