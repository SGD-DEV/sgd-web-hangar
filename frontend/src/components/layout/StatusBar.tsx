interface ServiceStatus {
  name: string
  status: string
  port: number
  version: string
}

interface StatusBarProps {
  services: Record<string, ServiceStatus>
}

export default function StatusBar({ services }: StatusBarProps) {
  const entries = Object.values(services)
  const running = entries.filter(s => s.status === 'running').length
  const total = entries.length

  return (
    <div className="h-7 bg-bg-sidebar border-t border-border flex items-center justify-between px-4 flex-shrink-0">
      <div className="flex items-center gap-4 text-xs text-text-dim">
        <span className="font-mono">v1.0.0</span>
        <span>
          {running > 0 ? (
            <span className="text-status-green">● {running}/{total} services running</span>
          ) : (
            <span className="text-text-dim">○ No services running</span>
          )}
        </span>
      </div>
      <div className="flex items-center gap-2 text-xs text-text-dim">
        <span className="font-mono">MCP :3742</span>
      </div>
    </div>
  )
}
