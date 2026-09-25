package app

// app_git.go: the Git dialog of a project - status, pull, commit + push,
// and turning an existing project into a repository. Authentication is left
// to Git Credential Manager (ships with Git for Windows), which works for
// GitHub, GitLab, Gitea and friends and keeps tokens in Windows' vault.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/devour-app/devour/app/projects"
	"github.com/devour-app/devour/app/services"
)

// GitInfo is what the Git dialog shows.
type GitInfo struct {
	Available   bool     `json:"available"` // git.exe found
	IsRepo      bool     `json:"is_repo"`
	Branch      string   `json:"branch"`
	Remote      string   `json:"remote"`
	HasUpstream bool     `json:"has_upstream"`
	Ahead       int      `json:"ahead"`
	Behind      int      `json:"behind"`
	Changes     []string `json:"changes"`
	LastCommit  string   `json:"last_commit"`
	UserName    string   `json:"user_name"`
	UserEmail   string   `json:"user_email"`
}

func (a *App) git(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// No console to type into: fail instead of hanging on a prompt. GCM
	// still opens its own login window when a remote needs credentials.
	cmd.Env = append(a.buildEnv(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	services.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("git %s timed out", args[0])
	}
	if err != nil {
		return string(out), fmt.Errorf("%s", gitErrorText(out, err))
	}
	return strings.TrimSpace(string(out)), nil
}

// gitErrorText picks the useful part of git's output for an error message.
func gitErrorText(out []byte, err error) string {
	var lines []string
	for _, l := range strings.Split(string(bytes.TrimSpace(out)), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "hint:") {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return err.Error()
	}
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, "\n")
}

func (a *App) gitProjectDir(name string) (string, error) {
	p, err := a.projectManager.Get(name)
	if err != nil {
		return "", err
	}
	if p.Path == "" {
		return "", fmt.Errorf("project %s has no folder", name)
	}
	return p.Path, nil
}

// isRepoRoot reports whether dir itself is the top of a work tree - not
// just a folder somewhere inside another repository.
func (a *App) isRepoRoot(dir string) bool {
	top, err := a.git(dir, 10*time.Second, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(filepath.FromSlash(top)), filepath.Clean(dir))
}

// GitStatus reports the repository state; with fetch it first asks the
// remote for new commits so "behind" is current.
func (a *App) GitStatus(name string, fetch bool) (GitInfo, error) {
	info := GitInfo{}
	if _, err := exec.LookPath("git"); err != nil {
		return info, nil
	}
	info.Available = true
	info.UserName, _ = a.git("", 5*time.Second, "config", "--global", "user.name")
	info.UserEmail, _ = a.git("", 5*time.Second, "config", "--global", "user.email")
	dir, err := a.gitProjectDir(name)
	if err != nil {
		return info, err
	}
	if !a.isRepoRoot(dir) {
		return info, nil
	}
	info.IsRepo = true
	if fetch {
		if _, err := a.git(dir, 2*time.Minute, "fetch", "--prune"); err != nil {
			return info, fmt.Errorf("fetch: %w", err)
		}
	}
	info.Branch, _ = a.git(dir, 10*time.Second, "branch", "--show-current")
	info.Remote, _ = a.git(dir, 10*time.Second, "remote", "get-url", "origin")
	if counts, err := a.git(dir, 10*time.Second, "rev-list", "--left-right", "--count", "HEAD...@{u}"); err == nil {
		info.HasUpstream = true
		if f := strings.Fields(counts); len(f) == 2 {
			info.Ahead, _ = strconv.Atoi(f[0])
			info.Behind, _ = strconv.Atoi(f[1])
		}
	}
	if st, err := a.git(dir, 30*time.Second, "status", "--porcelain", "--untracked-files=all"); err == nil && st != "" {
		info.Changes = strings.Split(st, "\n")
		if len(info.Changes) > 200 {
			info.Changes = append(info.Changes[:200], fmt.Sprintf("... and %d more", len(info.Changes)-200))
		}
	}
	info.LastCommit, _ = a.git(dir, 10*time.Second, "log", "-1", "--format=%h %s (%cr, %an)")
	return info, nil
}

// GitPull fast-forwards the current branch. Diverged branches are refused
// rather than merged, so nothing surprising happens on a live site.
func (a *App) GitPull(name string) (string, error) {
	dir, err := a.gitProjectDir(name)
	if err != nil {
		return "", err
	}
	return a.git(dir, 5*time.Minute, "pull", "--ff-only")
}

// GitCommitPush commits every change (if any) and pushes the branch, setting
// its upstream on the first push.
func (a *App) GitCommitPush(name, message string) (string, error) {
	dir, err := a.gitProjectDir(name)
	if err != nil {
		return "", err
	}
	excludeSecretFiles(dir)
	var log []string
	st, err := a.git(dir, 30*time.Second, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if st != "" {
		message = strings.TrimSpace(message)
		if message == "" {
			return "", fmt.Errorf("a commit message is required")
		}
		if _, err := a.git(dir, time.Minute, "add", "-A"); err != nil {
			return "", err
		}
		out, err := a.git(dir, time.Minute, "commit", "-m", message)
		if err != nil {
			return "", err
		}
		log = append(log, out)
	}
	var out string
	if _, err := a.git(dir, 10*time.Second, "rev-parse", "--abbrev-ref", "@{u}"); err == nil {
		out, err = a.git(dir, 5*time.Minute, "push")
		if err != nil {
			return strings.Join(log, "\n"), err
		}
	} else {
		out, err = a.git(dir, 5*time.Minute, "push", "-u", "origin", "HEAD")
		if err != nil {
			return strings.Join(log, "\n"), err
		}
	}
	log = append(log, out)
	return strings.Join(log, "\n"), nil
}

// GitInit turns the project folder into a repository on branch main and
// optionally connects it to an (empty) remote repository.
func (a *App) GitInit(name, remoteURL string) error {
	dir, err := a.gitProjectDir(name)
	if err != nil {
		return err
	}
	var remote string
	if strings.TrimSpace(remoteURL) != "" {
		u, err := validateGitURL(remoteURL)
		if err != nil {
			return err
		}
		remote = u.String()
	}
	if !a.isRepoRoot(dir) {
		if _, err := a.git(dir, 30*time.Second, "init", "-b", "main"); err != nil {
			return err
		}
	}
	excludeSecretFiles(dir)
	if remote != "" {
		return a.GitSetRemote(name, remote)
	}
	return nil
}

// GitSetRemote sets (or replaces) the origin URL.
func (a *App) GitSetRemote(name, remoteURL string) error {
	dir, err := a.gitProjectDir(name)
	if err != nil {
		return err
	}
	u, err := validateGitURL(remoteURL)
	if err != nil {
		return err
	}
	if _, err := a.git(dir, 10*time.Second, "remote", "get-url", "origin"); err == nil {
		_, err = a.git(dir, 10*time.Second, "remote", "set-url", "origin", u.String())
		return err
	}
	_, err = a.git(dir, 10*time.Second, "remote", "add", "origin", u.String())
	return err
}

// SetGitIdentity sets the name and e-mail written into commits.
func (a *App) SetGitIdentity(name, email string) error {
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if name == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("name and e-mail address are required")
	}
	if _, err := a.git("", 5*time.Second, "config", "--global", "user.name", name); err != nil {
		return err
	}
	_, err := a.git("", 5*time.Second, "config", "--global", "user.email", email)
	return err
}

// excludeSecretFiles lists the files Hangar writes passwords into in
// .git/info/exclude, so "commit everything" never pushes them. The exclude
// file is local to this clone and doesn't change the repository.
func excludeSecretFiles(dir string) {
	path := filepath.Join(dir, ".git", "info", "exclude")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var add []string
	for _, f := range projects.SecretFiles {
		if pattern := "/" + f; !have[pattern] {
			add = append(add, pattern)
		}
	}
	if len(add) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "# Hangar: files with passwords (database / mail settings)\n" + strings.Join(add, "\n") + "\n"
	_ = os.WriteFile(path, []byte(content), 0644)
}
