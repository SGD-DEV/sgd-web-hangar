import { Server, Code2, FolderOpen, Shield, Database, Settings, Package, Terminal, Route, LayoutGrid, Cloud } from 'lucide-react'
import type { NavItem } from '../../App'

interface SidebarProps {
  activeNav: NavItem
  onNavChange: (nav: NavItem) => void
}

const navItems: { id: NavItem; label: string; icon: typeof Server }[] = [
  { id: 'servers', label: 'Servers', icon: Server },
  { id: 'packages', label: 'Packages', icon: Package },
  { id: 'installed', label: 'Installed', icon: LayoutGrid },
  { id: 'php', label: 'PHP', icon: Code2 },
  { id: 'projects', label: 'Projects', icon: FolderOpen },
  { id: 'databases', label: 'Databases', icon: Database },
  { id: 'tunnel', label: 'Tunnel', icon: Cloud },
  { id: 'ssl', label: 'SSL', icon: Shield },
  { id: 'terminal', label: 'Terminal', icon: Terminal },
  { id: 'syspath', label: 'PATH', icon: Route },
  { id: 'settings', label: 'Settings', icon: Settings },
]

export default function Sidebar({ activeNav, onNavChange }: SidebarProps) {
  return (
    <div className="w-60 bg-bg-sidebar border-r border-border flex flex-col flex-shrink-0">
      <nav className="flex-1 py-4">
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
      </nav>

      <div className="p-4 border-t border-border">
        <div className="text-xs text-text-dim">
          <p>Hangar v1.0.0</p>
        </div>
      </div>
    </div>
  )
}
