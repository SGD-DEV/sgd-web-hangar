package app

// app_databases.go: the hosting-side database tools - create a database with
// its own user (what every CMS install asks for), drop, back up, and open
// phpMyAdmin / Adminer / pgAdmin without extra setup.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/devour-app/devour/app/dbinspect"
	"github.com/devour-app/devour/app/projects"
	"github.com/devour-app/devour/app/services"
)

var validDBIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

var systemDatabases = map[string]bool{
	"mysql": true, "information_schema": true, "performance_schema": true, "sys": true,
	"postgres": true, "template0": true, "template1": true,
}

// DatabaseCredentials is returned after creating a database so the UI can
// show (and copy) exactly what to paste into a CMS installer.
type DatabaseCredentials struct {
	Type     string `json:"type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
}

func (a *App) dbPort(dbType string) int {
	cfg, _ := a.config.GetAppConfig()
	if dbType == "mysql" {
		if cfg.MySQLPort > 0 {
			return cfg.MySQLPort
		}
		return 3306
	}
	if cfg.PostgreSQLPort > 0 {
		return cfg.PostgreSQLPort
	}
	return 5432
}

func (a *App) requireRunning(dbType string) error {
	st, err := a.serviceManager.Status(dbType)
	if err != nil {
		return err
	}
	if st.Status != services.StatusRunning {
		return fmt.Errorf("%s is not running - start it on the Servers page first", dbType)
	}
	return nil
}

func (a *App) execSQL(dbType, database, sql string) error {
	dsn, err := a.dbDSN(dbType, database)
	if err != nil {
		return err
	}
	_, err = dbinspect.RunQuery(dbType, dsn, sql)
	return err
}

func randomPassword() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sqlString quotes a value as a SQL string literal (both dialects).
func sqlString(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", "''") + "'"
}

// CreateDatabase creates a database plus a user that owns it. An empty user
// defaults to the database name, an empty password is generated.
func (a *App) CreateDatabase(dbType, name, user, password string) (DatabaseCredentials, error) {
	dbType = strings.ToLower(dbType)
	if dbType != "mysql" && dbType != "postgresql" {
		return DatabaseCredentials{}, fmt.Errorf("unknown database type %q", dbType)
	}
	if !validDBIdent.MatchString(name) {
		return DatabaseCredentials{}, fmt.Errorf("database name may contain letters, digits and _ (max 63, not starting with a digit)")
	}
	if systemDatabases[strings.ToLower(name)] {
		return DatabaseCredentials{}, fmt.Errorf("%q is a reserved system database", name)
	}
	if user == "" {
		user = name
	}
	if !validDBIdent.MatchString(user) || user == "root" || user == "postgres" {
		return DatabaseCredentials{}, fmt.Errorf("invalid user name %q", user)
	}
	if password == "" {
		password = randomPassword()
	}
	if err := a.requireRunning(dbType); err != nil {
		return DatabaseCredentials{}, err
	}

	var stmts []string
	switch dbType {
	case "mysql":
		stmts = []string{
			fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", name),
		}
		for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
			stmts = append(stmts,
				fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY %s", user, host, sqlString(password)),
				fmt.Sprintf("ALTER USER '%s'@'%s' IDENTIFIED BY %s", user, host, sqlString(password)),
				fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'", name, user, host),
			)
		}
		stmts = append(stmts, "FLUSH PRIVILEGES")
	case "postgresql":
		stmts = []string{
			fmt.Sprintf(`DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = %s) THEN CREATE ROLE "%s" LOGIN; END IF; END $$`, sqlString(user), user),
			fmt.Sprintf(`ALTER ROLE "%s" WITH LOGIN PASSWORD %s`, user, sqlString(password)),
			fmt.Sprintf(`CREATE DATABASE "%s" OWNER "%s" ENCODING 'UTF8'`, name, user),
		}
	}
	for _, s := range stmts {
		if err := a.execSQL(dbType, "", s); err != nil {
			return DatabaseCredentials{}, fmt.Errorf("%s: %w", strings.SplitN(s, " ", 3)[0]+" "+strings.SplitN(s, " ", 3)[1], err)
		}
	}
	return DatabaseCredentials{
		Type: dbType, Host: "127.0.0.1", Port: a.dbPort(dbType),
		Database: name, User: user, Password: password,
	}, nil
}

// DropDatabase deletes a database (never a system one). A backup is taken
// first so a mis-click is recoverable; its path is returned.
func (a *App) DropDatabase(dbType, name string) (string, error) {
	dbType = strings.ToLower(dbType)
	if !validDBIdent.MatchString(name) || systemDatabases[strings.ToLower(name)] {
		return "", fmt.Errorf("refusing to drop %q", name)
	}
	if err := a.requireRunning(dbType); err != nil {
		return "", err
	}
	backup, err := a.BackupDatabase(dbType, name)
	if err != nil {
		return "", fmt.Errorf("backup before drop failed, nothing was deleted: %w", err)
	}
	var sql string
	switch dbType {
	case "mysql":
		sql = fmt.Sprintf("DROP DATABASE `%s`", name)
	case "postgresql":
		sql = fmt.Sprintf(`DROP DATABASE "%s" WITH (FORCE)`, name)
	default:
		return "", fmt.Errorf("unknown database type %q", dbType)
	}
	if err := a.execSQL(dbType, "", sql); err != nil {
		return backup, err
	}
	return backup, nil
}

// BackupsDir is where database dumps go.
func (a *App) BackupsDir() string {
	return filepath.Join(a.paths.DataPath(), "backups")
}

// BackupDatabase dumps one database to data/backups/<type>/<name>-<time>.sql
// and returns the file path.
func (a *App) BackupDatabase(dbType, name string) (string, error) {
	dbType = strings.ToLower(dbType)
	if !validDBIdent.MatchString(name) {
		return "", fmt.Errorf("invalid database name %q", name)
	}
	if err := a.requireRunning(dbType); err != nil {
		return "", err
	}
	dir := filepath.Join(a.BackupsDir(), dbType)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	out := filepath.Join(dir, fmt.Sprintf("%s-%s.sql", name, time.Now().Format("20060102-150405")))
	port := fmt.Sprintf("%d", a.dbPort(dbType))

	var cmd *exec.Cmd
	switch dbType {
	case "mysql":
		exe := a.findDBBinary("mysql", "mysqldump.exe")
		if exe == "" {
			return "", fmt.Errorf("mysqldump.exe not found")
		}
		cmd = exec.Command(exe, "-h", "127.0.0.1", "-P", port, "-u", "root",
			"--single-transaction", "--routines", "--triggers", "--events",
			"--default-character-set=utf8mb4", "--result-file="+out, name)
	case "postgresql":
		exe := a.findDBBinary("postgresql", "pg_dump.exe")
		if exe == "" {
			return "", fmt.Errorf("pg_dump.exe not found")
		}
		cmd = exec.Command(exe, "-h", "127.0.0.1", "-p", port, "-U", "postgres",
			"--clean", "--if-exists", "--no-owner", "-f", out, name)
	default:
		return "", fmt.Errorf("unknown database type %q", dbType)
	}
	services.HideWindow(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("%s", strings.TrimSpace(firstLine(string(output), err.Error())))
	}
	return out, nil
}

func firstLine(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return strings.SplitN(s, "\n", 2)[0]
}

// findDBBinary locates a client tool (mysqldump.exe, pg_dump.exe, ...) in the
// configured install of service.
func (a *App) findDBBinary(service, exe string) string {
	var roots []string
	if cfg, err := a.config.GetServiceConfig(service); err == nil && cfg.InstallPath != "" {
		roots = append(roots, cfg.InstallPath)
	}
	base := filepath.Join(a.paths.InstalledPath(), service)
	if entries, err := os.ReadDir(base); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				roots = append(roots, filepath.Join(base, e.Name()))
			}
		}
	}
	for _, root := range roots {
		candidates := []string{filepath.Join(root, "bin", exe), filepath.Join(root, "pgsql", "bin", exe)}
		if entries, err := os.ReadDir(root); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					candidates = append(candidates,
						filepath.Join(root, e.Name(), "bin", exe),
						filepath.Join(root, e.Name(), "pgsql", "bin", exe))
				}
			}
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	return ""
}

// ListBackups returns the dump files, newest first.
func (a *App) ListBackups() []map[string]interface{} {
	var out []map[string]interface{}
	for _, t := range []string{"mysql", "postgresql"} {
		entries, _ := os.ReadDir(filepath.Join(a.BackupsDir(), t))
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || e.IsDir() {
				continue
			}
			out = append(out, map[string]interface{}{
				"type": t, "file": e.Name(),
				"path":     filepath.Join(a.BackupsDir(), t, e.Name()),
				"size":     info.Size(),
				"mod_time": info.ModTime().Format(time.RFC3339),
			})
		}
	}
	// newest first
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j]["mod_time"].(string) > out[j-1]["mod_time"].(string); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// --- Web tools ---

// ensureWebServer makes sure the active web server runs (tools are served by it).
func (a *App) ensureWebServer() error {
	active := a.GetActiveWebServer()
	if st, err := a.serviceManager.Status(active); err == nil && st.Status == services.StatusRunning {
		return a.projectManager.EnsureProjectsReady()
	}
	return a.SwitchWebServer(active)
}

// ensureToolProject registers a local-only project for a bundled web tool.
func (a *App) ensureToolProject(name, domain, dir string) error {
	existing, err := a.projectManager.Get(name)
	if err == nil && existing.Name != "" {
		changed := false
		if !existing.LocalOnly {
			existing.LocalOnly = true
			changed = true
		}
		if existing.Path != dir {
			existing.Path = dir
			existing.DocumentRoot = dir
			changed = true
		}
		if changed {
			return a.projectManager.Update(name, existing)
		}
		return nil
	}
	_, err = a.projectManager.CreateWithOptions(projects.CreateOptions{
		Name: name, Path: dir, Domain: domain, Framework: "php", LocalOnly: true,
	})
	return err
}

// OpenAdminer serves Adminer (MySQL + PostgreSQL in one page) at
// http://adminer.db, downloading the single-file app on first use. A small
// wrapper allows the password-less local root/postgres logins Hangar sets up.
func (a *App) OpenAdminer() (string, error) {
	dir := filepath.Join(a.paths.DataPath(), "tools", "adminer")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	app := filepath.Join(dir, "adminer.php")
	if _, err := os.Stat(app); err != nil {
		if err := downloadFile("https://github.com/vrana/adminer/releases/download/v5.4.2/adminer-5.4.2.php", app); err != nil {
			return "", fmt.Errorf("downloading Adminer: %w", err)
		}
	}
	index := `<?php
// Generated by Hangar. Adminer refuses empty passwords by default; Hangar's
// local MySQL root / PostgreSQL postgres accounts have none, and this site is
// only reachable from this machine (Require local).
function adminer_object() {
    class HangarAdminer extends Adminer\Adminer {
        function login($login, $password) { return true; }
    }
    return new HangarAdminer;
}
include __DIR__ . '/adminer.php';
`
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte(index), 0644); err != nil {
		return "", err
	}
	if err := a.ensureToolProject("adminer", "adminer.db", dir); err != nil {
		return "", fmt.Errorf("registering adminer.db: %w", err)
	}
	if err := a.ensureWebServer(); err != nil {
		return "", err
	}
	return a.projectURL("adminer"), nil
}

// projectURL is the local address of a project, https when it has a cert.
func (a *App) projectURL(name string) string {
	p, err := a.projectManager.Get(name)
	if err != nil {
		return "http://" + name + ".test"
	}
	if p.SSLEnabled && p.SSLCertPath != "" {
		return "https://" + p.Domain
	}
	return "http://" + p.Domain
}

// OpenPgAdmin launches the pgAdmin 4 desktop app bundled with PostgreSQL.
func (a *App) OpenPgAdmin() error {
	var exe string
	base := filepath.Join(a.paths.InstalledPath(), "postgresql")
	entries, _ := os.ReadDir(base)
	for i := len(entries) - 1; i >= 0 && exe == ""; i-- {
		for _, c := range []string{
			filepath.Join(base, entries[i].Name(), "pgsql", "pgAdmin 4", "runtime", "pgAdmin4.exe"),
			filepath.Join(base, entries[i].Name(), "pgAdmin 4", "runtime", "pgAdmin4.exe"),
		} {
			if _, err := os.Stat(c); err == nil {
				exe = c
				break
			}
		}
	}
	if exe == "" {
		return fmt.Errorf("pgAdmin 4 not found - it ships with the PostgreSQL package")
	}
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// writePhpMyAdminConfig creates config.inc.php so phpMyAdmin logs in to the
// local MySQL as root without asking. Safe only because the phpmyadmin.db
// site is local-only.
func writePhpMyAdminConfig(dir string, port int) error {
	path := filepath.Join(dir, "config.inc.php")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	cfg := fmt.Sprintf(`<?php
// Generated by Hangar: auto-login as the local MySQL root user. The
// phpmyadmin.db site only accepts requests from this machine.
$cfg['blowfish_secret'] = '%s';
$i = 1;
$cfg['Servers'][$i]['auth_type'] = 'config';
$cfg['Servers'][$i]['host'] = '127.0.0.1';
$cfg['Servers'][$i]['port'] = '%d';
$cfg['Servers'][$i]['user'] = 'root';
$cfg['Servers'][$i]['password'] = '';
$cfg['Servers'][$i]['AllowNoPassword'] = true;
$cfg['TempDir'] = __DIR__ . '/tmp';
`, hex.EncodeToString(secret), port)
	_ = os.MkdirAll(filepath.Join(dir, "tmp"), 0755)
	return os.WriteFile(path, []byte(cfg), 0644)
}

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 10 * time.Minute} // WordPress is ~30 MB
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
