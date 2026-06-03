# Changelog

All notable changes to Hangar are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added — v1.2 "Beyond PHP" track

- **Headless CLI mode** — `hangar.exe start mysql`, `hangar status`,
  `hangar list projects`, etc. work in any terminal without the GUI
  open. Subcommands: `start`, `stop`, `restart`, `status`, `list`,
  `services`, `version`, `daemon`. Aliases: `pg` → `postgresql`,
  `mongo` → `mongodb`, `meili` → `meilisearch`.
- **Daemon mode** — `hangar daemon` keeps the MCP server on
  127.0.0.1:3742 reachable and a tray icon visible without opening
  the GUI window. AI agents stay connected across reboots.
- **Shared `app/core` package** — same bootstrap wiring backs the
  GUI, CLI, and daemon. No more "MCP only works when the GUI is
  open."
- **3 new services** — MongoDB 8.0, Meilisearch 1.45, Caddy 2.11.
  Full Start/Stop/Restart/Logs lifecycle. Meilisearch ships with a
  local-dev master key; users override for non-dev use.
- **11 new packages** in the registry:
  - **Node.js**: nvm-windows 1.2.2, fnm 1.39.0, Bun 1.3.14
  - **Python**: Astral uv 0.11.18
  - **PHP tools**: Adminer 5.4.2, WP-CLI 2.12.0, Symfony CLI 5.17.1
  - **Cloud / tools**: GitHub CLI 2.93.0, Cloudflared 2026.5.2,
    Supabase CLI 2.104.0, Fly.io flyctl 0.4.57
- **New package categories**: `search`, `python`, `cloud`. Existing
  `nodejs` and `tools` get more populated.
- **Installed page**: groups the new categories and rows for every
  new tool with the right launch behaviour (service vs CLI).

### Deferred — not shipped in v1.2, planned for v1.3 or v1.4

- **Microsoft Garnet (Redis-protocol)** — package zip extracts with
  the executable under `net8.0/`, and the framework-dependent build
  requires .NET 8 runtime. Hangar has no prereq layer yet to pull
  in .NET 8 automatically. Once we add prereq handling Garnet ships
  in v1.3.
- **MinIO** — latest stable has a CVE; the version one release back
  does not yet have a Windows binary published. Waiting on upstream
  to publish a clean Windows release.
- **Typesense** — vendor does not publish a Windows native binary;
  Windows path is Docker-only. Out of scope until WSL bundling.
- **AWS CLI, OpenSSL, Tailscale** — all MSI installers. Hangar's
  package layer only handles `.zip`/`.tar.gz` extraction today.
  Adding MSI install support is planned for v1.3.
- **Laravel Installer, Cloudflare Wrangler, Vercel CLI, Netlify
  CLI** — composer-global / npm-global installs. Need a new install
  flow distinct from URL download. Planned for v1.4.

### Fixed

- `hangar.exe` no longer falls through to the GUI when given an
  unknown CLI subcommand. Prints a clear error and exits 1.

## [1.0.0] — 2026-05-19

Initial public release.

### Fixed (pre-release polish)

- Embedded `mkcert.exe` so SSL works on a fresh install without the user having to track down the binary separately.
- MCP server now hardened against DNS-rebinding (Host + Origin must resolve to `localhost` / `127.0.0.1` / `::1`).
- `Server.Start` binds the listener synchronously so port conflicts surface as real errors instead of silent "running" + connection refused.
- 5 s shutdown ceiling on the MCP server — a hung SSE client no longer blocks app quit.
- Strict domain validation on project create to prevent vhost-file injection.
- TCP-probe on MySQL service before reporting "ready" to avoid race conditions on startup.
- Apache + Nginx vhost linker reworked for cleaner regeneration + error reporting.

### Added

- **Service manager** — start / stop / restart Apache, Nginx, MySQL, PostgreSQL, Mailpit from the UI with live status, port, uptime, and PID.
- **Multi-version PHP** — install PHP 7.3, 8.1, 8.2, 8.3, 8.4, 8.5 side by side and switch globally or per project. Auto-generated `php.ini` with a Composer-compatible extension set.
- **Project manager** — auto-detects Laravel, WordPress, Symfony, generic PHP, static, Node, and PocketBase projects. Generates Apache and Nginx vhosts from templates, adds `*.test` to the hosts file, exposes per-vhost access/error/SSL logs.
- **Local HTTPS** via mkcert — one-click toggle per project, regenerates the vhost and reloads the web server.
- **DNS server** — built-in `*.test` resolver answering `127.0.0.1`, with system-DNS fallback.
- **MCP server** on `127.0.0.1:3742` exposing 14 tools (`list_services`, `start_service`, `stop_service`, `restart_service`, `switch_php`, `list_projects`, `create_project`, `get_logs`, `list_site_logs`, `get_site_logs`, `set_project_ssl`, `run_sql`, `inspect_database`, `get_connection_string`).
- **Server-side database auditor** — `inspect_database` walks MySQL or PostgreSQL schemas with ~30 deterministic rules (orphan FKs, money-as-float, plaintext secrets, missing indexes, multi-tenancy gaps, PCI/PHI checks) and writes a runnable `.sql` migration file under `%LOCALAPPDATA%\Hangar\data\audits\`.
- **Package manager** — registry-based installer for PHP, databases, web servers, Node, Go, and developer tools. Live progress (MB done / total / MB·s / ETA). "Add by URL" for anything not in the registry.
- **Installed launcher page** — single view of every installed package grouped by category with one-click Launch and Open Folder actions per item.
- **HeidiSQL 12.17** portable bundle with pre-filled session launcher for each running database.
- **phpMyAdmin** as a managed project — auto-vhosted at `http://phpmyadmin.test` when you click Open.
- **External terminal integration** — opens Windows Terminal with the active project's PHP, Composer, Node, and Go on PATH.
- **System tray + single-instance lock** — closes to tray while services run; relaunching focuses the existing window.
- **First-run wizard** that installs a sensible default bundle.
- **System PATH editor**, per-service config editor, and in-memory log viewer.

### Security

- Default service credentials are local-bind only (`root` on MySQL, `postgres` on Postgres).
- MCP server is bound to `127.0.0.1` and not reachable from other hosts on the network.
- mkcert root CA is installed only on explicit SSL enable, never silently.

### Known limitations

- Windows only. macOS and Linux are on the roadmap.
- The installer is not yet code-signed; Windows SmartScreen will warn on first install.
- The in-app terminal is a simple line-mode shell — use "Open in Terminal" for full PTY behaviour.

[Unreleased]: https://github.com/Anssol-Labs/hangar/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/Anssol-Labs/hangar/releases/tag/v1.0.0
