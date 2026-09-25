import { useState, useEffect, useCallback } from 'react'
import { ExternalLink, Play, Folder, Terminal as TerminalIcon, RefreshCw, Server, Package2 } from 'lucide-react'

// InstalledItem mirrors app.InstalledItem on the Go side. Keep shapes in
// sync if you add fields there.
interface InstalledItem {
  name: string
  label: string
  version: string
  category: string
  install_path: string
  launch_kind: 'exe' | 'web' | 'service' | 'session' | 'cli' | 'runtime' | 'none'
  exe_path?: string
  web_url?: string
  service_name?: string
  is_active: boolean
}

// Display order + labels for the category groups. Items in unknown
// categories fall through into an "Other" bucket so we don't drop them
// silently if the backend adds something new.
const categoryOrder: { id: string; label: string; description: string }[] = [
  { id: 'tools',     label: 'Tools',         description: 'Datenbank-Clients, Editoren, Hilfsprogramme' },
  { id: 'database',  label: 'Datenbanken',   description: 'MySQL, PostgreSQL, MongoDB' },
  { id: 'search',    label: 'Suche',         description: 'Meilisearch und andere' },
  { id: 'php',       label: 'PHP',           description: 'PHP-Laufzeitumgebungen' },
  { id: 'webserver', label: 'Webserver',     description: 'Apache, Nginx, Caddy' },
  { id: 'nodejs',    label: 'Node.js',       description: 'Node, Bun, Versionsverwaltung' },
  { id: 'python',    label: 'Python',        description: 'Python-Werkzeuge (uv)' },
  { id: 'cloud',     label: 'Cloud',         description: 'Deploy CLIs (Cloudflared, Supabase, Fly.io)' },
  { id: 'golang',    label: 'Go',            description: 'Go-Werkzeuge' },
  { id: 'other',     label: 'Sonstiges',     description: '' },
]

function categoryOf(item: InstalledItem): string {
  return categoryOrder.find(c => c.id === item.category)?.id || 'other'
}

export default function InstalledPage() {
  const [items, setItems] = useState<InstalledItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<string>('')

  const load = useCallback(async () => {
    try {
      // @ts-ignore Wails bindings
      const fn = window.go?.app?.App?.ListInstalledItems
      if (!fn) {
        setError('Backend noch nicht bereit - lade die Seite neu, sobald Hangar fertig gestartet ist.')
        setLoading(false)
        return
      }
      const data = await fn()
      setItems(data || [])
      setError('')
    } catch (e: any) {
      setError(String(e?.message || e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  async function launch(item: InstalledItem) {
    setBusy(item.name + '-' + item.version)
    try {
      switch (item.launch_kind) {
        case 'web': {
          if (item.name === 'phpmyadmin') {
            // @ts-ignore
            const url = await window.go.app.App.OpenPhpMyAdmin()
            // Open in the default browser via Wails BrowserOpenURL if exposed,
            // otherwise fall back to window.open which webview2 routes to the
            // system browser for http(s).
            // @ts-ignore
            if (window.runtime?.BrowserOpenURL) window.runtime.BrowserOpenURL(url)
            else window.open(url, '_blank')
          } else if (item.web_url) {
            // @ts-ignore
            if (window.runtime?.BrowserOpenURL) window.runtime.BrowserOpenURL(item.web_url)
            else window.open(item.web_url, '_blank')
          }
          break
        }
        case 'exe': {
          // @ts-ignore
          await window.go.app.App.LaunchTool(item.name, '')
          break
        }
        case 'session': {
          // HeidiSQL session manager
          // @ts-ignore
          await window.go.app.App.OpenHeidiSQL()
          break
        }
        case 'service': {
          // Toggle/start the service from the Services page contextually
          // @ts-ignore
          await window.go.app.App.StartService(item.service_name || item.name)
          break
        }
        case 'cli':
        case 'runtime':
        case 'none':
        default:
          // No actionable launch; the install-path button still works.
          break
      }
    } catch (e: any) {
      setError(String(e?.message || e))
    } finally {
      setBusy('')
    }
  }

  async function openFolder(item: InstalledItem) {
    if (!item.install_path) return
    try {
      // @ts-ignore
      const fn = window.go?.app?.App?.OpenInExplorer
      if (fn) await fn(item.install_path)
      else if ((window as any).runtime?.BrowserOpenURL) {
        // Fallback: open as file:// URL
        ;(window as any).runtime.BrowserOpenURL('file:///' + item.install_path.replace(/\\/g, '/'))
      }
    } catch (e: any) {
      setError(String(e?.message || e))
    }
  }

  // Group by display category
  const grouped: Record<string, InstalledItem[]> = {}
  for (const it of items) {
    const cat = categoryOf(it)
    if (!grouped[cat]) grouped[cat] = []
    grouped[cat].push(it)
  }
  for (const cat in grouped) {
    grouped[cat].sort((a, b) => a.label.localeCompare(b.label))
  }

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-5xl mx-auto px-6 py-6">
        {/* Header */}
        <div className="flex items-start justify-between mb-6">
          <div>
            <h1 className="text-lg font-semibold text-text-primary">Installiert</h1>
            <p className="text-sm text-text-muted mt-1">
              Alles, was installiert ist, nach Kategorien sortiert. <span className="text-accent">Öffnen</span> startet ein Programm,
              <span className="text-accent">Ordner</span> zeigt sein Installationsverzeichnis.
            </p>
          </div>
          <button
            onClick={load}
            className="flex items-center gap-2 px-3 py-1.5 rounded text-xs text-text-muted hover:text-text-primary hover:bg-bg-secondary/60 transition-colors"
          >
            <RefreshCw size={14} strokeWidth={1.5} />
            Aktualisieren
          </button>
        </div>

        {error && (
          <div className="mb-4 p-3 rounded border border-status-red/40 bg-status-red/10 text-status-red text-sm">
            {error}
          </div>
        )}

        {loading ? (
          <p className="text-text-muted text-sm">Installierte Pakete werden geladen…</p>
        ) : items.length === 0 ? (
          <div className="rounded-lg border border-border bg-bg-secondary/30 p-8 text-center">
            <Package2 size={28} strokeWidth={1.5} className="mx-auto text-text-muted mb-2" />
            <p className="text-text-primary text-sm font-medium">Noch nichts installiert</p>
            <p className="text-text-muted text-xs mt-1">
              Unter <span className="text-accent">Pakete</span> in der Seitenleiste kannst du Tools, PHP-Versionen und Datenbanken herunterladen.
            </p>
          </div>
        ) : (
          <div className="space-y-8">
            {categoryOrder.map(cat => {
              const list = grouped[cat.id]
              if (!list || list.length === 0) return null
              return (
                <section key={cat.id}>
                  <div className="flex items-baseline gap-3 mb-3">
                    <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider">
                      {cat.label}
                    </h2>
                    <span className="text-xs text-text-dim">{list.length} item{list.length === 1 ? '' : 's'}</span>
                  </div>
                  <div className="space-y-2">
                    {list.map(item => (
                      <InstalledRow
                        key={item.name + '-' + item.version}
                        item={item}
                        busy={busy === item.name + '-' + item.version}
                        onLaunch={() => launch(item)}
                        onOpenFolder={() => openFolder(item)}
                      />
                    ))}
                  </div>
                </section>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

interface RowProps {
  item: InstalledItem
  busy: boolean
  onLaunch: () => void
  onOpenFolder: () => void
}

function InstalledRow({ item, busy, onLaunch, onOpenFolder }: RowProps) {
  const launchInfo = launchLabel(item)
  return (
    <div className="rounded border border-border bg-bg-secondary/30 hover:bg-bg-secondary/50 px-4 py-3 flex items-center gap-4">
      <div className="w-9 h-9 rounded bg-bg-primary border border-border flex items-center justify-center text-text-muted">
        {iconFor(item)}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-text-primary truncate">{item.label}</span>
          {item.is_active && (
            <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-accent/15 text-accent">AKTIV</span>
          )}
        </div>
        <div className="text-xs text-text-dim font-mono truncate mt-0.5">
          {item.name} v{item.version}{item.install_path ? `  •  ${item.install_path}` : ''}
        </div>
      </div>
      <div className="flex items-center gap-2 flex-shrink-0">
        <button
          onClick={onOpenFolder}
          title="Installationsordner im Explorer öffnen"
          className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded text-text-muted hover:text-text-primary hover:bg-bg-secondary border border-border transition-colors"
        >
          <Folder size={13} strokeWidth={1.5} />
          Ordner
        </button>
        {launchInfo.actionable ? (
          <button
            onClick={onLaunch}
            disabled={busy}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded bg-accent/15 text-accent hover:bg-accent/25 disabled:opacity-50 transition-colors"
          >
            {launchInfo.icon}
            {busy ? 'Wird geöffnet…' : launchInfo.label}
          </button>
        ) : (
          <span className="text-xs text-text-dim italic px-2 py-1.5">{launchInfo.label}</span>
        )}
      </div>
    </div>
  )
}

function launchLabel(item: InstalledItem): { actionable: boolean; label: string; icon: JSX.Element | null } {
  switch (item.launch_kind) {
    case 'web':
      return { actionable: true, label: 'Öffnen', icon: <ExternalLink size={13} strokeWidth={1.5} /> }
    case 'exe':
      return { actionable: !!item.exe_path, label: 'Starten', icon: <Play size={13} strokeWidth={1.5} /> }
    case 'session':
      return { actionable: !!item.exe_path, label: 'Sitzungen öffnen', icon: <Play size={13} strokeWidth={1.5} /> }
    case 'service':
      return { actionable: true, label: 'Dienst starten', icon: <Server size={13} strokeWidth={1.5} /> }
    case 'cli':
      return { actionable: false, label: 'Nur Kommandozeile - Terminal nutzen', icon: <TerminalIcon size={13} strokeWidth={1.5} /> }
    case 'runtime':
      return { actionable: false, label: 'Laufzeitumgebung', icon: null }
    case 'none':
    default:
      return { actionable: false, label: 'Nicht startbar', icon: null }
  }
}

function iconFor(item: InstalledItem): JSX.Element {
  // Tiny short-letter badge keeps the look consistent with the Packages page.
  const text = (
    item.name === 'phpmyadmin' ? 'PMA' :
    item.name === 'adminer' ? 'ADM' :
    item.name === 'heidisql' ? 'HDS' :
    item.name === 'dbeaver' ? 'DBV' :
    item.name === 'vscode' ? 'VSC' :
    item.name === 'pocketbase' ? 'PB' :
    item.name === 'composer' ? 'CMP' :
    item.name === 'wp-cli' ? 'WP' :
    item.name === 'symfony-cli' ? 'SYM' :
    item.name === 'gh' ? 'GH' :
    item.name === 'mailpit' ? 'MP' :
    item.name === 'meilisearch' ? 'MS' :
    item.name === 'mongodb' ? 'MGO' :
    item.name === 'mysql' ? 'SQL' :
    item.name === 'postgresql' ? 'PG' :
    item.name === 'apache' ? 'AP' :
    item.name === 'nginx' ? 'NX' :
    item.name === 'caddy' ? 'CAD' :
    item.name === 'php' ? 'PHP' :
    item.name === 'node' ? 'JS' :
    item.name === 'bun' ? 'BUN' :
    item.name === 'nvm-windows' ? 'NVM' :
    item.name === 'fnm' ? 'FNM' :
    item.name === 'uv' ? 'UV' :
    item.name === 'supabase' ? 'SB' :
    item.name === 'flyctl' ? 'FLY' :
    item.name === 'cloudflared' ? 'CF' :
    item.name === 'go' || item.name === 'golang' ? 'GO' :
    '?'
  )
  return <span className="text-[10px] font-mono">{text}</span>
}
