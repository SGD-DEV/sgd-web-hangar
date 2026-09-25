package app

// app_project_setup.go: per-project database and mail settings, the German
// WordPress installer and creating a project from a git URL.

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/devour-app/devour/app/downloader"
	"github.com/devour-app/devour/app/projects"
	"github.com/devour-app/devour/app/services"
)

// --- Database ----------------------------------------------------------------

var nonIdentChars = regexp.MustCompile(`[^a-z0-9_]+`)

// projectDBName turns a project name into a database/user name. MySQL user
// names are limited to 32 characters.
func projectDBName(project string) string {
	s := strings.Trim(nonIdentChars.ReplaceAllString(strings.ToLower(project), "_"), "_")
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		s = "p_" + s
	}
	if len(s) > 32 {
		s = strings.TrimRight(s[:32], "_")
	}
	return s
}

// saveProjectConfig stores the project and writes its settings into the
// project's config files. Returns the saved project and the files written.
func (a *App) saveProjectConfig(p projects.Project) (projects.Project, []string, error) {
	files, err := projects.ApplyConfig(p)
	if err != nil {
		return p, files, err
	}
	if err := a.projectManager.Update(p.Name, p); err != nil {
		return p, files, err
	}
	// An app gets its database and mail settings as environment variables.
	if p.App != nil && a.appInstalled(p.Name) {
		if err := a.applyAppService(p, true); err != nil {
			return p, files, fmt.Errorf("saved, but updating the app service failed: %w", err)
		}
		files = append(files, "service environment")
	}
	return p, files, nil
}

// ProjectSetupResult is what the Database and Mail dialogs get back.
type ProjectSetupResult struct {
	Project projects.Project `json:"project"`
	Files   []string         `json:"files"`
}

// CreateProjectDatabase creates a database plus user named after the
// project, assigns it and writes it into wp-config.php / .env.
func (a *App) CreateProjectDatabase(name, dbType string) (ProjectSetupResult, error) {
	p, err := a.projectManager.Get(name)
	if err != nil {
		return ProjectSetupResult{}, err
	}
	if p.IsWordPress() && dbType != "mysql" {
		return ProjectSetupResult{}, fmt.Errorf("WordPress only runs on MySQL")
	}
	dbName := projectDBName(name)
	creds, err := a.CreateDatabase(dbType, dbName, dbName, "")
	if err != nil {
		return ProjectSetupResult{}, err
	}
	p.Database = &projects.Database{Type: creds.Type, Host: creds.Host, Port: creds.Port, Name: creds.Database, User: creds.User, Password: creds.Password}
	p, files, err := a.saveProjectConfig(p)
	return ProjectSetupResult{Project: p, Files: files}, err
}

// SetProjectDatabase assigns an existing database (e.g. one restored from a
// backup) or, with db == nil, removes the assignment. The database itself
// and the project's config files are left alone when removing.
func (a *App) SetProjectDatabase(name string, db *projects.Database) (ProjectSetupResult, error) {
	p, err := a.projectManager.Get(name)
	if err != nil {
		return ProjectSetupResult{}, err
	}
	if db == nil {
		p.Database = nil
		err := a.projectManager.Update(name, p)
		return ProjectSetupResult{Project: p}, err
	}
	if db.Type != "mysql" && db.Type != "postgresql" {
		return ProjectSetupResult{}, fmt.Errorf("unknown database type %q", db.Type)
	}
	if strings.TrimSpace(db.Name) == "" || strings.TrimSpace(db.User) == "" {
		return ProjectSetupResult{}, fmt.Errorf("database name and user are required")
	}
	if db.Host == "" {
		db.Host = "127.0.0.1"
	}
	if db.Port == 0 {
		db.Port = a.dbPort(db.Type)
	}
	p.Database = db
	p, files, err := a.saveProjectConfig(p)
	return ProjectSetupResult{Project: p, Files: files}, err
}

// --- Mail --------------------------------------------------------------------

// SetProjectMail stores the project's SMTP settings (nil = Mailpit) and
// writes them into the mu-plugin (WordPress) or .env.
func (a *App) SetProjectMail(name string, m *projects.Mail) (ProjectSetupResult, error) {
	p, err := a.projectManager.Get(name)
	if err != nil {
		return ProjectSetupResult{}, err
	}
	if m != nil {
		if err := validateMail(m); err != nil {
			return ProjectSetupResult{}, err
		}
	}
	p.Mail = m
	p, files, err := a.saveProjectConfig(p)
	return ProjectSetupResult{Project: p, Files: files}, err
}

func validateMail(m *projects.Mail) error {
	m.Host = strings.TrimSpace(m.Host)
	m.User = strings.TrimSpace(m.User)
	m.FromEmail = strings.TrimSpace(m.FromEmail)
	m.FromName = strings.TrimSpace(m.FromName)
	if m.Host == "" {
		return fmt.Errorf("SMTP host is required")
	}
	if m.Port < 1 || m.Port > 65535 {
		return fmt.Errorf("SMTP port must be between 1 and 65535")
	}
	if m.Encryption != "" && m.Encryption != "tls" && m.Encryption != "ssl" {
		return fmt.Errorf("unknown encryption %q", m.Encryption)
	}
	if m.FromEmail != "" {
		if _, err := mail.ParseAddress(m.FromEmail); err != nil {
			return fmt.Errorf("sender address: %v", err)
		}
	}
	if strings.ContainsAny(m.FromName+m.Host+m.User, "\r\n") {
		return fmt.Errorf("line breaks are not allowed")
	}
	return nil
}

// SendProjectTestMail sends a short mail through the project's SMTP settings
// so a wrong password shows up here and not in a customer's contact form.
func (a *App) SendProjectTestMail(name, to string) error {
	p, err := a.projectManager.Get(name)
	if err != nil {
		return err
	}
	rcpt, err := mail.ParseAddress(strings.TrimSpace(to))
	if err != nil {
		return fmt.Errorf("recipient: %v", err)
	}
	m := p.EffectiveMail()
	from := m.FromEmail
	if from == "" {
		from = "hangar@localhost"
	}
	fromHeader := from
	if m.FromName != "" {
		fromHeader = mime.QEncoding.Encode("utf-8", m.FromName) + " <" + from + ">"
	}
	body := strings.Join([]string{
		"From: " + fromHeader,
		"To: " + rcpt.Address,
		"Subject: " + mime.QEncoding.Encode("utf-8", "Hangar Testmail: "+p.Name),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Diese Testmail wurde von Hangar über die Mail-Einstellungen des Projekts " + p.Name + " gesendet.",
		fmt.Sprintf("Server: %s:%d", m.Host, m.Port),
		"",
	}, "\r\n")
	return sendSMTP(m, from, rcpt.Address, []byte(body))
}

func sendSMTP(m projects.Mail, from, to string, msg []byte) error {
	addr := net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if m.Encryption == "ssl" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: m.Host})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(45 * time.Second))
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if m.Encryption == "tls" {
		if err := c.StartTLS(&tls.Config{ServerName: m.Host}); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if m.User != "" {
		// PlainAuth refuses to send the password over an unencrypted
		// connection to anything but localhost.
		if err := c.Auth(smtp.PlainAuth("", m.User, m.Password, m.Host)); err != nil {
			return fmt.Errorf("login: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("sender rejected: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("recipient rejected: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// --- WordPress ---------------------------------------------------------------

const wordPressGermanURL = "https://de.wordpress.org/latest-de_DE.zip"

// installWordPress downloads the current German WordPress into projectPath,
// registers the project, gives it a MySQL database when MySQL is running
// and routes its mail to Mailpit. It returns a message for the user.
func (a *App) installWordPress(name, projectPath string) (string, error) {
	root := filepath.Dir(projectPath)
	// Work next to the target (same drive, so the final move is a rename)
	// and not in %TEMP%.
	tmp, err := os.MkdirTemp(root, ".hangar-wp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	archive := filepath.Join(tmp, "wordpress.zip")
	if err := downloadFile(wordPressGermanURL, archive); err != nil {
		return "", fmt.Errorf("downloading WordPress: %w", err)
	}
	if err := downloader.ExtractZip(archive, tmp); err != nil {
		return "", fmt.Errorf("extracting WordPress: %w", err)
	}
	if err := os.Rename(filepath.Join(tmp, "wordpress"), projectPath); err != nil {
		return "", fmt.Errorf("moving WordPress into place: %w", err)
	}

	p, err := a.projectManager.CreateWithOptions(projects.CreateOptions{Name: name, Path: projectPath, Framework: string(projects.FrameworkWordPress)})
	if err != nil {
		return "", err
	}
	if err := a.ensureWordPressExtensions(p.PHPVersion); err != nil {
		a.logStore.AddWithLevel("hangar", "WordPress: enabling PHP extensions: "+err.Error(), "warn")
	}

	msg := "WordPress (Deutsch) installiert."
	if st, err := a.serviceManager.Status("mysql"); err == nil && st.Status == services.StatusRunning {
		if _, err := a.CreateProjectDatabase(name, "mysql"); err != nil {
			msg += " Datenbank konnte nicht angelegt werden: " + err.Error()
		} else {
			msg += " Datenbank angelegt und in wp-config.php eingetragen."
		}
	} else {
		msg += " MySQL läuft nicht - Datenbank bitte später über das Datenbank-Symbol anlegen."
		if _, _, err := a.saveProjectConfig(p); err != nil {
			a.logStore.AddWithLevel("hangar", "WordPress: mail plugin: "+err.Error(), "warn")
		}
	}
	return msg, nil
}

// ensureWordPressExtensions enables the PHP extensions WordPress needs
// (mysqli is required, the rest are what Site Health asks for).
func (a *App) ensureWordPressExtensions(version string) error {
	if a.phpManager == nil {
		return nil
	}
	if version == "" {
		version = a.phpManager.ActiveVersion()
	}
	want := map[string]bool{"mysqli": true, "gd": true, "exif": true, "intl": true, "zip": true, "curl": true, "mbstring": true, "fileinfo": true, "openssl": true}
	for _, ext := range a.phpManager.GetExtensions(version) {
		if ext.Enabled {
			delete(want, ext.Name)
		}
	}
	for _, ext := range a.phpManager.GetExtensions(version) {
		if want[ext.Name] {
			if err := a.TogglePHPExtension(version, ext.Name, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// --- Clone from git ------------------------------------------------------------

var validProjectName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// CloneOptions is the "Clone from URL" form.
type CloneOptions struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// validateGitURL accepts https URLs without embedded credentials: a token in
// the URL would end up in plain text in .git/config. Private repos log in
// through Git Credential Manager instead.
func validateGitURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || strings.Trim(u.Path, "/") == "" {
		return nil, fmt.Errorf("only https:// repository URLs are supported, e.g. https://github.com/user/repo.git")
	}
	if u.User != nil {
		return nil, fmt.Errorf("don't put credentials in the URL - Git asks for them when needed and stores them in Windows")
	}
	return u, nil
}

// ProjectNameFromGitURL suggests a project name for a repository URL.
func ProjectNameFromGitURL(raw string) string {
	u, err := validateGitURL(raw)
	if err != nil {
		return ""
	}
	base := strings.TrimSuffix(filepath.Base(strings.TrimRight(u.Path, "/")), ".git")
	s := strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(base), "-"), "-")
	if len(s) > 63 {
		s = s[:63]
	}
	return s
}

// CloneProject clones a repository into the projects folder and registers
// it as a project.
func (a *App) CloneProject(opts CloneOptions) (projects.Project, error) {
	u, err := validateGitURL(opts.URL)
	if err != nil {
		return projects.Project{}, err
	}
	name := strings.TrimSpace(strings.ToLower(opts.Name))
	if name == "" {
		name = ProjectNameFromGitURL(opts.URL)
	}
	if !validProjectName.MatchString(name) {
		return projects.Project{}, fmt.Errorf("project name may contain lowercase letters, digits and -")
	}
	if _, err := a.projectManager.Get(name); err == nil {
		return projects.Project{}, fmt.Errorf("a project named %s already exists", name)
	}
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		path = filepath.Join(a.paths.ProjectsPath(), name)
	}
	if entries, err := os.ReadDir(path); err == nil && len(entries) > 0 {
		return projects.Project{}, fmt.Errorf("folder %s already exists and is not empty", path)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "clone", "--", u.String(), path)
	cmd.Env = append(a.buildEnv(), "GIT_TERMINAL_PROMPT=0")
	services.HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(path)
		return projects.Project{}, fmt.Errorf("git clone failed: %s", gitErrorText(out, err))
	}
	excludeSecretFiles(path)

	return a.projectManager.CreateWithOptions(projects.CreateOptions{Name: name, Path: path})
}
