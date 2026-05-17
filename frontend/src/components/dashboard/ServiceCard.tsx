interface ServiceStatus {
  name: string
  status: 'running' | 'stopped' | 'starting' | 'error'
  port: number
  version: string
}

interface ServiceCardProps {
  name: string
  status?: ServiceStatus
  selected: boolean
  onClick: () => void
}

const statusColors: Record<string, string> = {
  running: 'bg-status-green',
  stopped: 'bg-status-red',
  starting: 'bg-status-yellow animate-pulse-dot',
  error: 'bg-status-red',
}

const serviceLabels: Record<string, string> = {
  apache: 'Apache HTTP',
  nginx: 'Nginx',
  mysql: 'MySQL',
  postgresql: 'PostgreSQL',
  mailpit: 'Mailpit',
}

export default function ServiceCard({ name, status, selected, onClick }: ServiceCardProps) {
  const dotColor = status ? statusColors[status.status] || 'bg-text-dim' : 'bg-text-dim'
  const label = serviceLabels[name] || name

  return (
    <button
      onClick={onClick}
      className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-all text-left
        ${selected
          ? 'bg-bg-selected border border-accent/20'
          : 'hover:bg-bg-secondary border border-transparent'
        }`}
    >
      <div className={`w-2 h-2 rounded-full flex-shrink-0 ${dotColor}`} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center justify-between">
          <span className={`text-sm font-medium truncate ${selected ? 'text-text-primary' : 'text-text-muted'}`}>
            {label}
          </span>
          {status?.version && (
            <span className="text-xs text-text-dim font-mono ml-2">{status.version}</span>
          )}
        </div>
        <div className="flex items-center gap-2 mt-0.5">
          <span className="text-xs text-text-dim capitalize">{status?.status || 'unknown'}</span>
          {status?.port ? (
            <span className="text-xs text-text-dim font-mono">:{status.port}</span>
          ) : null}
        </div>
      </div>
    </button>
  )
}
