# Changelog

All notable changes to Hangar are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

_Nothing yet._

## [1.0.0] — 2026-05-18

Initial public release.

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
