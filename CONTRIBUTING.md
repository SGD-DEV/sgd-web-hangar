# Contributing to Hangar

Thanks for your interest in improving Hangar. This guide covers how to get a working dev environment, the branching workflow we use, and the standards a pull request should meet before it lands on `main`.

## Table of contents

1. [Code of conduct](#code-of-conduct)
2. [Reporting bugs and requesting features](#reporting-bugs-and-requesting-features)
3. [Development environment](#development-environment)
4. [Branching workflow](#branching-workflow)
5. [Coding standards](#coding-standards)
6. [Commit messages](#commit-messages)
7. [Pull request checklist](#pull-request-checklist)
8. [Areas we'd love help on](#areas-wed-love-help-on)

---

## Code of conduct

Be respectful. Disagreements about technical direction are healthy; personal attacks are not. We follow the spirit of the [Contributor Covenant](https://www.contributor-covenant.org/). Maintainers reserve the right to close issues or PRs that violate it.

## Reporting bugs and requesting features

- **Bugs** — use the *Bug report* template under [Issues → New Issue](https://github.com/Anssol-Labs/hangar/issues/new/choose). Include Hangar version, Windows build, steps to reproduce, expected vs. actual, and the relevant lines from `%LOCALAPPDATA%\Hangar\data\logs\` if applicable.
- **Feature requests** — use the *Feature request* template. Describe the use case, not just the implementation. "I want X" is less useful than "When I do Y, I have to do Z, which is painful because…".
- **Security vulnerabilities** — DO NOT open a public issue. See [SECURITY.md](SECURITY.md).

## Development environment

### Prerequisites

| Tool | Minimum version | Notes |
|---|---|---|
| Go | 1.22 | `go.mod` will bump the toolchain to 1.24+ on first build |
| Node.js | 18 LTS | Needed only for the frontend |
| Wails CLI | latest | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| WebView2 Runtime | — | Pre-installed on Windows 10/11; on Windows Server install manually |
| NSIS | 3.x | Only needed if you want to build the installer (`wails build -nsis`) |

### One-time setup

```powershell
git clone https://github.com/Anssol-Labs/hangar.git
cd hangar
cd frontend
npm install
cd ..
```

### Run the app in dev mode

```powershell
wails dev
```

This compiles the Go backend, starts Vite for the frontend, and opens the app window with hot reload on the React side. Backend changes require restarting `wails dev`.

### Build a production binary

```powershell
# Just the .exe
wails build

# Full Windows installer
wails build -nsis
# → build/bin/hangar-amd64-installer.exe
```

### Where data lives

Hangar writes everything user-specific to `%LOCALAPPDATA%\Hangar\`:

- `data\hangar.db` — bbolt config store
- `data\installed\` — downloaded packages (PHP, MySQL, etc.)
- `data\logs\` — service logs
- `data\ssl\` — mkcert-generated certificates
- `data\conf\` — generated Apache/Nginx/MySQL/Postgres configs
- `data\audits\` — output from `inspect_database` MCP tool

If you want a clean slate, delete that folder and relaunch — the first-run wizard will reappear.

## Branching workflow

We use a lightweight **GitHub Flow**:

1. Fork the repo (external contributors) or create a branch (collaborators).
2. Branch naming:
   - `feature/<short-name>` — new functionality
   - `fix/<short-name>` — bug fixes
   - `docs/<short-name>` — README / docs changes only
   - `chore/<short-name>` — build, CI, dependency bumps
3. Open a PR against `main` early — draft PRs are welcome.
4. At least one approving review from a maintainer is required before merge.
5. Squash-and-merge is the default; the commit message should follow the convention below.

`main` is protected: no direct pushes, no force-pushes, no merges without a PR.

## Coding standards

### Go

- Run `go fmt ./...` and `go vet ./...` before pushing. CI will block on either.
- Prefer the standard library when reasonable; new third-party dependencies need justification in the PR description.
- Keep packages small and focused. `app/dbinspect` shouldn't know about `app/mcp` — wire them in `app/app.go` instead.
- Errors propagate up with `fmt.Errorf("context: %w", err)`. Don't drop error info with `_ = err`.
- Public functions get a comment explaining *why* the function exists, not just what it does.
- Long-running work goes in goroutines with explicit cancellation via `context.Context`.

### Frontend

- TypeScript strict mode is on. Don't disable it locally.
- Components live under `frontend/src/components/<feature>/`. One component per file.
- TailwindCSS for styling. Don't add new CSS files unless absolutely necessary.
- Lucide React for icons — keep the visual language consistent.
- Run `npx tsc --noEmit` before pushing.

### General

- No emoji in commits, code, or comments unless the user explicitly requests it.
- No AI-attribution trailers (`Co-Authored-By: <some bot>`). If you used AI to draft a PR, that's fine — review it carefully and sign it as your own work.
- Don't commit secrets, API keys, or `.env` files. The `.gitignore` should already catch the common ones; if you find a gap, fix it.

## Commit messages

Format:

```
<area>: short summary in imperative mood

Optional longer explanation of *why*. Wrap at 72 columns. Reference issues
with "Fixes #123" or "Refs #456" so they auto-close on merge.
```

Examples:

```
mcp: add inspect_database tool with server-side rule engine
projects: fix SSL toggle leaving stale cert on disable
docs: clarify mkcert root CA install step in quick start
```

Areas roughly map to package or top-level dir: `mcp`, `dbinspect`, `projects`, `services`, `php`, `frontend`, `installer`, `docs`, `ci`, etc.

## Pull request checklist

Before requesting review, confirm:

- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] `cd frontend && npx tsc --noEmit` passes
- [ ] Tests added or updated for the change (if applicable)
- [ ] Manually tested in `wails dev` against a clean `%LOCALAPPDATA%\Hangar\` if the change touches startup, config, or paths
- [ ] CHANGELOG.md updated under `## [Unreleased]` with a one-line note
- [ ] No commits authored by a bot or AI assistant
- [ ] PR description explains the motivation, not just the code change

## Areas we'd love help on

Good first issues are tagged [`good-first-issue`](https://github.com/Anssol-Labs/hangar/labels/good-first-issue). Some larger items on the roadmap:

- macOS port (Wails supports it; we need someone with the hardware)
- Linux port (likely .deb + AppImage)
- Code-signing the Windows installer so the SmartScreen warning goes away
- Real PTY in the in-app terminal (currently shells out to Windows Terminal)
- Additional framework detectors (Strapi, Astro, Hono, …)
- A `backup_database` MCP tool that produces a `mysqldump` / `pg_dump` next to the audit output
- Headless mode + CLI for CI use cases

Open a discussion before starting anything large so we can sync on direction.

---

Thanks again. Hangar is a small project today; every contribution moves it forward.
