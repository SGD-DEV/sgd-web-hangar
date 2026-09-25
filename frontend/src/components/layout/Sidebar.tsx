import { useEffect, useState } from 'react'
import { Server, Code2, FolderOpen, Shield, Database, Settings, Package, Terminal, Route, LayoutGrid, Cloud, Palette, ExternalLink, KeyRound } from 'lucide-react'
import type { NavItem } from '../../App'
import { useAppearance } from '../../lib/theme'
import { call, toast, errorMessage } from '../../lib/api'

interface WebTool {
  id: string
  label: string
  url: string
  installed: boolean
  running: boolean
  key?: string
}

// Quick links to the browser UIs of the services - easy to forget where
// Mailpit or Meilisearch live otherwise.
function WebTools() {
  const [tools, setTools] = useState<WebTool[]>([])

  useEffect(() => {
    const load = () => call<WebTool[]>('GetWebTools').then(t => setTools(t || [])).catch(() => {})
    load()
    const timer = setInterval(load, 10000)
    return () => clearInterval(timer)
  }, [])

  async function open(url: string | Promise<string>) {
    try {
      await call('OpenURL', await url)
    } catch (e) {
      toast.error('Konnte nicht geöffnet werden', errorMessage(e))
    }
  }

  const links: { id: string; label: string; title: string; dot?: boolean; onOpen: () => void; keyToCopy?: string }[] = [
    ...tools.filter(t => t.installed).map(t => ({
      id: t.id,
      label: t.label,
      title: t.running ? `${t.url} öffnen` : `${t.label} läuft nicht - starte es auf der Seite Server`,
      dot: t.running,
      onOpen: () => open(t.url),
      keyToCopy: t.key,
    })),
    { id: 'phpmyadmin', label: 'phpMyAdmin', title: 'phpMyAdmin (MySQL) öffnen', onOpen: () => open(call<string>('OpenPhpMyAdmin')) },
    { id: 'adminer', label: 'Adminer', title: 'Adminer (MySQL, PostgreSQL) öffnen', onOpen: () => open(call<string>('OpenAdminer')) },
  ]

  return (
    <div className="mt-4 pt-4 border-t border-border">
      <div className="px-4 mb-2">
        <span className="text-xs font-medium text-text-dim uppercase tracking-wider">Web-Oberflächen</span>
      </div>
      {links.map(l => (
        <div key={l.id} className="group flex items-center px-4 hover:bg-bg-secondary/50">
          <button onClick={l.onOpen} title={l.title}
            className="flex-1 flex items-center gap-3 py-1.5 text-xs text-text-muted group-hover:text-text-primary text-left">
            <ExternalLink size={13} strokeWidth={1.5} />
            <span className="flex-1">{l.label}</span>
            {l.dot !== undefined && <span className={`w-1.5 h-1.5 rounded-full ${l.dot ? 'bg-status-green' : 'bg-text-dim'}`} />}
          </button>
          {l.keyToCopy && (
            <button
              onClick={() => navigator.clipboard.writeText(l.keyToCopy!).then(() => toast.info(`${l.label}-Schlüssel kopiert`, 'Im Anmeldefeld der Oberfläche einfügen.')).catch(() => {})}
              title="API-Schlüssel kopieren (die Oberfläche fragt danach)"
              className="ml-2 p-1 text-text-dim hover:text-text-primary">
              <KeyRound size={12} />
            </button>
          )}
        </div>
      ))}
    </div>
  )
}

interface SidebarProps {
  activeNav: NavItem
  onNavChange: (nav: NavItem) => void
}

const navItems: { id: NavItem; label: string; icon: typeof Server }[] = [
  { id: 'servers', label: 'Server', icon: Server },
  { id: 'packages', label: 'Pakete', icon: Package },
  { id: 'installed', label: 'Installiert', icon: LayoutGrid },
  { id: 'php', label: 'PHP', icon: Code2 },
  { id: 'projects', label: 'Projekte', icon: FolderOpen },
  { id: 'databases', label: 'Datenbanken', icon: Database },
  { id: 'tunnel', label: 'Tunnel', icon: Cloud },
  { id: 'ssl', label: 'SSL', icon: Shield },
  { id: 'terminal', label: 'Terminal', icon: Terminal },
  { id: 'syspath', label: 'PATH', icon: Route },
  { id: 'appearance', label: 'Erscheinungsbild', icon: Palette },
  { id: 'settings', label: 'Einstellungen', icon: Settings },
]

export default function Sidebar({ activeNav, onNavChange }: SidebarProps) {
  const appearance = useAppearance(s => s.settings)
  const name = appearance?.app_name || 'Hangar'
  return (
    <div className="w-60 bg-bg-sidebar border-r border-border flex flex-col flex-shrink-0">
      <div className="flex items-center gap-3 px-4 h-14 border-b border-border">
        {appearance?.logo
          ? <img src={appearance.logo} alt="" className="h-8 max-w-[64px] object-contain flex-shrink-0" />
          : <div className="w-8 h-8 rounded-lg bg-accent text-on-accent flex items-center justify-center text-sm font-semibold flex-shrink-0">{name.charAt(0).toUpperCase()}</div>}
        <span className="text-sm font-medium text-text-primary truncate" title={name}>{name}</span>
      </div>
      <nav className="flex-1 py-4 overflow-y-auto">
        <div className="px-4 mb-4">
          <span className="text-xs font-medium text-text-dim uppercase tracking-wider">Navigation</span>
        </div>
        {navItems.map(item => {
          const Icon = item.icon
          const isActive = activeNav === item.id
          return (
            <button
              key={item.id}
              onClick={() => onNavChange(item.id)}
              className={`w-full flex items-center gap-3 px-4 py-2.5 text-sm transition-colors relative
                ${isActive
                  ? 'text-accent bg-bg-selected'
                  : 'text-text-muted hover:text-text-primary hover:bg-bg-secondary/50'
                }`}
            >
              {isActive && (
                <div className="absolute left-0 top-0 bottom-0 w-0.5 bg-accent" />
              )}
              <Icon size={16} strokeWidth={1.5} />
              <span>{item.label}</span>
            </button>
          )
        })}
        <WebTools />
      </nav>

      <div className="p-4 border-t border-border">
        <div className="text-xs text-text-dim">
          <p>{name !== 'Hangar' ? `${name} · ` : ''}Hangar v1.0.0</p>
        </div>
      </div>
    </div>
  )
}
