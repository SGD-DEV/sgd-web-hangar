# Security policy

Hangar manages local network ports, modifies the Windows hosts file, runs services with elevated privileges in some flows, and exposes a JSON-RPC server on `127.0.0.1:3742`. Bugs in any of those paths can have real consequences, so we take security reports seriously.

## Supported versions

We patch the latest minor release. Older versions get fixes only for critical issues at maintainer discretion.

| Version | Supported |
|---|---|
| 1.x (latest) | ✅ |
| < 1.0 | ❌ |

## Reporting a vulnerability

**Please do NOT open a public GitHub issue for security problems.** Public disclosure before a patch exists puts every Hangar user at risk.

Instead, email us at **security@anssol.tech** with:

- A description of the issue and its impact
- Steps to reproduce (proof-of-concept code or a screencast is ideal)
- The Hangar version, Windows build, and any relevant config
- Your name and a way to credit you in the release notes (optional)

If you don't get an acknowledgement within **3 business days**, please re-send or contact a maintainer directly via GitHub.

## What to expect from us

1. **Acknowledgement** within 3 business days.
2. **Initial assessment** within 7 days — severity (Critical/High/Medium/Low), affected versions, and a rough timeline.
3. **Patch + advisory** depending on severity:
   - Critical / High: targeted release within 14 days where feasible
   - Medium / Low: rolled into the next scheduled release
4. **Credit** in the release notes and CHANGELOG entry, unless you ask to remain anonymous.

## Coordinated disclosure

We support coordinated disclosure. Please give us a reasonable window (typically 30–90 days depending on severity) to ship a fix before publishing details. We will agree a public disclosure date with you.

## What's in scope

- Anything that ships in the official Hangar installer or binaries from this repo
- The MCP server's auth model and tool surface (`mcp__hangar__*`)
- Privilege escalation through the elevated-helper flow used for hosts-file edits and port binding
- Hosts-file or system PATH manipulation reachable without UAC
- Reading or writing files outside `%LOCALAPPDATA%\Hangar\` and a registered project's directory
- Plain-text credentials or tokens in BoltDB, logs, audit output, or generated configs
- DNS hijacking via the bundled DNS server
- Anything that lets a remote attacker reach Hangar's services on `127.0.0.1` from elsewhere

## What's out of scope

- Vulnerabilities in **bundled third-party software** (PHP, MySQL, PostgreSQL, Apache, Nginx, mkcert, phpMyAdmin, HeidiSQL, DBeaver, etc.). Report those upstream — we'll bump the bundled version when patched.
- Social engineering of Hangar maintainers
- Physical access to a user's machine
- Issues that require a user to install obviously malicious "Add by URL" packages
- DoS that requires the attacker to already have local code execution
- Missing security headers on local-only HTTP responses

## Hardening notes (for users)

Hangar is designed for **local development on a trusted machine**. If you must run it on a shared or untrusted network:

- Keep the MCP server bound to `127.0.0.1` (the default). Don't reverse-proxy it to a public address.
- Don't enable Windows Firewall exceptions for the bundled databases beyond `127.0.0.1`.
- Don't import "Add by URL" packages from sources you don't trust — they're downloaded and executed with your user's permissions.
- Default DB credentials (`root` on MySQL, `postgres` on Postgres, no password) are local-bind only. Change them if you must, but do not expose those ports.

---

Thank you for helping keep Hangar and its users safe.
