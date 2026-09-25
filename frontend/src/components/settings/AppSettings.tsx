import { useState, useEffect } from 'react'
import { call, toast, errorMessage } from '../../lib/api'

interface AppConfig {
  active_php: string
  projects_root: string
  domain_suffix?: string
  apache_port: number
  nginx_port: number
  mysql_port: number
  postgresql_port: number
  dns_port: number
  mcp_port: number
  auto_start_all: boolean
  apache_enabled: boolean
  nginx_enabled: boolean
  ssl_enabled: boolean
  theme: string
  php_workers?: number
  tunnel_config_path?: string
  tunnel_service_name?: string
  [key: string]: any
}

export default function AppSettings() {
  const [config, setConfig] = useState<AppConfig>({
    active_php: '',
    projects_root: '',
    apache_port: 80,
    nginx_port: 80,
    mysql_port: 3306,
    postgresql_port: 5432,
    dns_port: 53,
    mcp_port: 3742,
    auto_start_all: false,
    apache_enabled: true,
    nginx_enabled: true,
    ssl_enabled: false,
    theme: 'dark',
  })
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    loadConfig()
  }, [])

  async function loadConfig() {
    try {
      if (window.go?.app?.App?.GetConfig) {
        const cfg = await window.go.app.App.GetConfig()
        if (cfg) setConfig(cfg)
      }
    } catch (e) {
      console.error(e)
    }
  }

  async function handleSave() {
    setSaving(true)
    setSaved(false)
    try {
      await call('UpdateConfig', config)
      setSaved(true)
      toast.success('Einstellungen gespeichert', 'Änderungen an Ports und PHP-Workern gelten beim nächsten Start eines Webservers.')
      setTimeout(() => setSaved(false), 2000)
    } catch (e) {
      toast.error('Einstellungen nicht gespeichert', errorMessage(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="max-w-xl">
        <h2 className="text-lg font-medium mb-6">Einstellungen</h2>

        <div className="space-y-6">
          {/* Projects Root */}
          <div>
            <label className="block text-xs text-text-muted mb-1.5">Projektordner</label>
            <div className="flex gap-2">
              <input
                type="text"
                value={config.projects_root}
                onChange={e => setConfig({ ...config, projects_root: e.target.value })}
                placeholder="C:\Hosting\www"
                className="w-full px-3 py-2 bg-bg-secondary border border-border rounded-lg text-sm text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50 transition-colors font-mono select-text"
              />
              <button
                onClick={async () => {
                  try { const d = await call<string>('PickProjectDirectory'); if (d) setConfig({ ...config, projects_root: d }) } catch { /* cancelled */ }
                }}
                className="px-3 py-2 bg-bg-secondary border border-border rounded-lg text-xs text-text-muted hover:text-text-primary"
              >
                Durchsuchen…
              </button>
            </div>
            <p className="text-[11px] text-text-dim mt-1">Neue Projekte werden hier angelegt; „Suchen“ übernimmt vorhandene Ordner von hier.</p>
          </div>

          {/* Domain suffix */}
          <div>
            <label className="block text-xs text-text-muted mb-1.5">Domain-Endung für neue Projekte</label>
            <input
              type="text"
              value={config.domain_suffix || ''}
              onChange={e => setConfig({ ...config, domain_suffix: e.target.value })}
              placeholder=".test"
              className="w-40 px-3 py-2 bg-bg-secondary border border-border rounded-lg text-sm text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50 transition-colors font-mono select-text"
            />
            <p className="text-[11px] text-text-dim mt-1">
              Ein Projekt „shop“ wird zu <span className="font-mono">shop{config.domain_suffix || '.test'}</span>. Empfohlen: <span className="font-mono">.test</span> (dafür reserviert).
              <span className="font-mono"> .local</span> funktioniert hier, ist aber eigentlich für die Geräteerkennung im Netzwerk gedacht.
              Bestehende Projekte behalten ihre Domain.
            </p>
          </div>

          {/* Hosting */}
          <div className="pt-4 border-t border-border">
            <h3 className="text-sm font-medium mb-4">Hosting</h3>
            <div className="space-y-4">
              <div className="flex items-center gap-3">
                <input
                  type="checkbox"
                  id="auto-start-hosting"
                  checked={config.auto_start_all}
                  onChange={e => setConfig({ ...config, auto_start_all: e.target.checked })}
                  className="rounded border-border bg-bg-secondary accent-accent"
                />
                <label htmlFor="auto-start-hosting" className="text-sm text-text-muted cursor-pointer">
                  Beim Start von Hangar die Dienste starten, die vorher liefen (z. B. nach einem Neustart)
                </label>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-xs text-text-muted w-44">PHP-Worker pro Version:</span>
                <input
                  type="number" min={1} max={8}
                  value={config.php_workers || 8}
                  onChange={e => setConfig({ ...config, php_workers: Math.min(8, Math.max(1, parseInt(e.target.value) || 8)) })}
                  className="w-20 px-2 py-1 bg-bg-secondary border border-border rounded text-sm text-text-primary focus:outline-none focus:border-accent/50 font-mono"
                />
                <span className="text-[11px] text-text-dim">parallele PHP-Anfragen (1-8)</span>
              </div>
              <div>
                <label className="block text-xs text-text-muted mb-1.5">Konfigurationsdatei für den Cloudflare Tunnel</label>
                <input
                  type="text"
                  value={config.tunnel_config_path || ''}
                  onChange={e => setConfig({ ...config, tunnel_config_path: e.target.value })}
                  placeholder="(Standard: Hangar-Datenordner\cloudflared\config.yml)"
                  className="w-full px-3 py-2 bg-bg-secondary border border-border rounded-lg text-sm text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50 font-mono select-text"
                />
              </div>
              <div className="flex items-center gap-2">
                <span className="text-xs text-text-muted w-44">Windows-Dienst des Tunnels:</span>
                <input
                  type="text"
                  value={config.tunnel_service_name || 'Cloudflared'}
                  onChange={e => setConfig({ ...config, tunnel_service_name: e.target.value })}
                  className="w-48 px-2 py-1 bg-bg-secondary border border-border rounded text-sm text-text-primary focus:outline-none focus:border-accent/50 font-mono select-text"
                />
              </div>
            </div>
          </div>

          {/* Web Servers */}
          <div className="pt-4 border-t border-border">
            <h3 className="text-sm font-medium mb-4">Webserver</h3>
            <div className="space-y-4">
              {/* Apache */}
              <div className="flex items-center gap-4">
                <input
                  type="checkbox"
                  id="apache-enabled"
                  checked={config.apache_enabled}
                  onChange={e => setConfig({ ...config, apache_enabled: e.target.checked })}
                  className="rounded border-border bg-bg-secondary accent-accent"
                />
                <label htmlFor="apache-enabled" className="text-sm text-text-primary cursor-pointer w-20">Apache</label>
                <div className="flex items-center gap-2">
                  <span className="text-xs text-text-muted">Port:</span>
                  <input
                    type="number"
                    value={config.apache_port}
                    onChange={e => setConfig({ ...config, apache_port: parseInt(e.target.value) || 80 })}
                    className="w-20 px-2 py-1 bg-bg-secondary border border-border rounded text-sm text-text-primary focus:outline-none focus:border-accent/50 font-mono"
                  />
                </div>
              </div>

              {/* Nginx */}
              <div className="flex items-center gap-4">
                <input
                  type="checkbox"
                  id="nginx-enabled"
                  checked={config.nginx_enabled}
                  onChange={e => setConfig({ ...config, nginx_enabled: e.target.checked })}
                  className="rounded border-border bg-bg-secondary accent-accent"
                />
                <label htmlFor="nginx-enabled" className="text-sm text-text-primary cursor-pointer w-20">Nginx</label>
                <div className="flex items-center gap-2">
                  <span className="text-xs text-text-muted">Port:</span>
                  <input
                    type="number"
                    value={config.nginx_port}
                    onChange={e => setConfig({ ...config, nginx_port: parseInt(e.target.value) || 80 })}
                    className="w-20 px-2 py-1 bg-bg-secondary border border-border rounded text-sm text-text-primary focus:outline-none focus:border-accent/50 font-mono"
                  />
                </div>
              </div>
            </div>
          </div>

          {/* SSL */}
          <div className="pt-4 border-t border-border">
            <h3 className="text-sm font-medium mb-4">SSL / HTTPS</h3>
            <div className="flex items-center gap-3">
              <input
                type="checkbox"
                id="ssl-enabled"
                checked={config.ssl_enabled}
                onChange={e => setConfig({ ...config, ssl_enabled: e.target.checked })}
                className="rounded border-border bg-bg-secondary accent-accent"
              />
              <label htmlFor="ssl-enabled" className="text-sm text-text-muted cursor-pointer">
                SSL für Projekte aktivieren (erzeugt Zertifikate mit mkcert)
              </label>
            </div>
          </div>

          {/* Database Ports */}
          <div className="pt-4 border-t border-border">
            <h3 className="text-sm font-medium mb-4">Datenbank-Ports</h3>
            <div className="flex items-center gap-6">
              <div className="flex items-center gap-2">
                <span className="text-xs text-text-muted">MySQL:</span>
                <input
                  type="number"
                  value={config.mysql_port}
                  onChange={e => setConfig({ ...config, mysql_port: parseInt(e.target.value) || 3306 })}
                  className="w-20 px-2 py-1 bg-bg-secondary border border-border rounded text-sm text-text-primary focus:outline-none focus:border-accent/50 font-mono"
                />
              </div>
              <div className="flex items-center gap-2">
                <span className="text-xs text-text-muted">PostgreSQL:</span>
                <input
                  type="number"
                  value={config.postgresql_port}
                  onChange={e => setConfig({ ...config, postgresql_port: parseInt(e.target.value) || 5432 })}
                  className="w-20 px-2 py-1 bg-bg-secondary border border-border rounded text-sm text-text-primary focus:outline-none focus:border-accent/50 font-mono"
                />
              </div>
            </div>
          </div>

          {/* Other */}
          <div className="pt-4 border-t border-border">
            <h3 className="text-sm font-medium mb-4">Sonstiges</h3>
            <div className="space-y-4">
              <div className="flex items-center gap-6">
                <div className="flex items-center gap-2">
                  <span className="text-xs text-text-muted">DNS Port:</span>
                  <input
                    type="number"
                    value={config.dns_port}
                    onChange={e => setConfig({ ...config, dns_port: parseInt(e.target.value) || 53 })}
                    className="w-20 px-2 py-1 bg-bg-secondary border border-border rounded text-sm text-text-primary focus:outline-none focus:border-accent/50 font-mono"
                  />
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-xs text-text-muted">MCP Port:</span>
                  <input
                    type="number"
                    value={config.mcp_port}
                    onChange={e => setConfig({ ...config, mcp_port: parseInt(e.target.value) || 3742 })}
                    className="w-20 px-2 py-1 bg-bg-secondary border border-border rounded text-sm text-text-primary focus:outline-none focus:border-accent/50 font-mono"
                  />
                </div>
              </div>

            </div>
          </div>

          <div className="pt-4 border-t border-border">
            <button
              onClick={handleSave}
              disabled={saving}
              className="px-4 py-2 bg-accent text-on-accent rounded-lg text-sm font-medium hover:bg-accent-hover transition-colors disabled:opacity-50"
            >
              {saving ? 'Speichert…' : saved ? 'Gespeichert!' : 'Einstellungen speichern'}
            </button>
          </div>
        </div>

        {/* MCP setup. Three popular AI editors connect over HTTP-SSE, just
            different config-file locations. Hangar's MCP server exposes the
            full service-control / project / DB tooling once any of these
            point at http://127.0.0.1:<mcp_port>/sse. */}
        <McpSetup mcpPort={config.mcp_port || 3742} />

        <div className="mt-10 pt-6 border-t border-border">
          <h3 className="text-sm font-medium mb-3">Über</h3>
          <div className="space-y-1 text-xs text-text-muted">
            <p><strong className="text-text-primary">Hangar</strong> v1.0.0</p>
            <p>Hosting-Panel für Windows (basiert auf Hangar, GPLv3)</p>
          </div>
        </div>
      </div>
    </div>
  )
}

// --- MCP setup card ---
//
// Hangar's MCP server exposes service control (start/stop/restart), project
// management, log fetching, and DB connection strings to any MCP-aware AI
// editor. Connection is HTTP+SSE at /sse on the configured MCP port (3742
// by default).
//
// This card renders three tabs - one per popular client - each with the
// config-file path on Windows and a copy-pasteable JSON snippet.

interface McpSetupProps {
  mcpPort: number
}

function McpSetup({ mcpPort }: McpSetupProps) {
  const [tab, setTab] = useState<'claude' | 'cursor' | 'windsurf'>('claude')
  const [copied, setCopied] = useState(false)

  const url = `http://127.0.0.1:${mcpPort}/sse`

  // Three editors, slightly different JSON shapes. All point at the same
  // /sse endpoint. The shapes match each editor's documented v1 MCP config.
  const configs: Record<typeof tab, { path: string; json: string; reload: string }> = {
    claude: {
      path: '%APPDATA%\\Claude\\claude_desktop_config.json',
      reload: 'Claude Desktop über das Tray-Symbol beenden und neu starten.',
      json: JSON.stringify(
        { mcpServers: { hangar: { type: 'sse', url } } },
        null,
        2,
      ),
    },
    cursor: {
      path: '%USERPROFILE%\\.cursor\\mcp.json',
      reload: 'Settings > MCP > Hangar aus- und wieder einschalten (oder Cursor neu starten).',
      json: JSON.stringify(
        { mcpServers: { hangar: { url } } },
        null,
        2,
      ),
    },
    windsurf: {
      path: '%USERPROFILE%\\.codeium\\windsurf\\mcp_config.json',
      reload: 'Cascade-Panel > MCP-Symbol > Refresh, oder Windsurf neu starten.',
      json: JSON.stringify(
        { mcpServers: { hangar: { serverUrl: url } } },
        null,
        2,
      ),
    },
  }

  const c = configs[tab]

  async function copyConfig() {
    try {
      await navigator.clipboard.writeText(c.json)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch { /* clipboard blocked in some webviews */ }
  }

  return (
    <div className="mt-10 pt-6 border-t border-border">
      <h3 className="text-sm font-medium mb-1">KI-Editor verbinden</h3>
      <p className="text-xs text-text-muted mb-4">
        Hangar betreibt einen MCP-Server unter <span className="font-mono text-text-primary">{url}</span>.
        Wähle deinen Editor und kopiere die Konfiguration unten. Verfügbare Werkzeuge: Dienste auflisten/starten/stoppen,
        Projekte auflisten/anlegen, Logs abrufen, Datenbank-Verbindungen abfragen und mehr.
      </p>

      <div className="flex gap-1 border-b border-border mb-4">
        {(['claude', 'cursor', 'windsurf'] as const).map(t => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-3 py-1.5 text-xs font-medium transition-colors -mb-px ${tab === t ? 'border-b-2 border-accent text-text-primary' : 'border-b-2 border-transparent text-text-muted hover:text-text-primary'}`}
          >
            {t === 'claude' ? 'Claude Desktop' : t === 'cursor' ? 'Cursor' : 'Windsurf'}
          </button>
        ))}
      </div>

      <div className="space-y-3">
        <div>
          <p className="text-xs text-text-muted mb-1">Pfad der Konfigurationsdatei</p>
          <code className="block px-3 py-2 bg-bg-secondary border border-border rounded text-xs font-mono text-text-primary">
            {c.path}
          </code>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1">
            <p className="text-xs text-text-muted">Hier einfügen (mit vorhandenen <code className="text-text-primary">mcpServers</code> zusammenführen, falls es welche gibt)</p>
            <button
              onClick={copyConfig}
              className="px-2 py-0.5 bg-accent/10 border border-accent/30 text-accent rounded text-[11px] hover:bg-accent/20"
            >
              {copied ? 'Kopiert!' : 'Kopieren'}
            </button>
          </div>
          <pre className="px-3 py-2 bg-bg-secondary border border-border rounded text-xs font-mono text-text-primary overflow-x-auto">
{c.json}
          </pre>
        </div>

        <div>
          <p className="text-xs text-text-muted mb-1">Änderung übernehmen</p>
          <p className="text-xs text-text-primary">{c.reload}</p>
        </div>

        <p className="text-[11px] text-text-dim">
          Der MCP-Server startet mit Hangar (Port {mcpPort}). Wenn dein Editor keine Verbindung bekommt,
          prüfe, ob Hangar läuft und die Windows-Firewall Port {mcpPort} auf localhost nicht blockiert.
        </p>
      </div>
    </div>
  )
}
