import { useState, useEffect, useCallback } from 'react'
import { Database, ExternalLink, RefreshCw, Box, Layers, Code } from 'lucide-react'

interface ServiceStatus {
  status: string
  port: number
  version: string
}

interface DBInfo {
  type: string
  label: string
  user: string
  defaultPort: number
  status: ServiceStatus | null
  databases: string[]
  loadingDbs: boolean
}

type ToolKey = 'heidisql' | 'dbeaver' | 'pocketbase' | 'vscode'

const EXTERNAL_TOOLS: { key: ToolKey; label: string; icon: any }[] = [
  { key: 'heidisql', label: 'HeidiSQL', icon: Database },
  { key: 'dbeaver', label: 'DBeaver', icon: Layers },
  { key: 'pocketbase', label: 'PocketBase', icon: Box },
  { key: 'vscode', label: 'VS Code', icon: Code },
]

export default function DatabasesPage({ services }: { services: Record<string, ServiceStatus> }) {
  const [dbInfos, setDbInfos] = useState<DBInfo[]>([
    { type: 'mysql', label: 'MySQL', user: 'root', defaultPort: 3306, status: null, databases: [], loadingDbs: false },
    { type: 'postgresql', label: 'PostgreSQL', user: 'postgres', defaultPort: 5432, status: null, databases: [], loadingDbs: false },
  ])
  const [installedTools, setInstalledTools] = useState<Record<ToolKey, boolean>>({
    heidisql: false, dbeaver: false, pocketbase: false, vscode: false,
  })

  const updateStatuses = useCallback(() => {
    setDbInfos(prev => prev.map(db => ({
      ...db,
      status: services[db.type] || null,
    })))
  }, [services])

  useEffect(() => {
    updateStatuses()
  }, [updateStatuses])

  // Probe which external tools are installed once on mount so we know
  // which launch buttons to render. Cheap (filesystem stat) so no debounce.
  useEffect(() => {
    let cancelled = false
    ;(async () => {
      const results: Record<ToolKey, boolean> = { heidisql: false, dbeaver: false, pocketbase: false, vscode: false }
      results.heidisql = !!(await window.go?.app?.App?.IsHeidiSQLInstalled?.().catch(() => false))
      for (const t of ['dbeaver', 'pocketbase', 'vscode'] as ToolKey[]) {
        results[t] = !!(await window.go?.app?.App?.IsToolInstalled?.(t).catch(() => false))
      }
      if (!cancelled) setInstalledTools(results)
    })()
    return () => { cancelled = true }
  }, [])

  async function launchTool(tool: ToolKey) {
    try {
      if (tool === 'heidisql') {
        await window.go?.app?.App?.OpenHeidiSQL?.()
      } else {
        await window.go?.app?.App?.LaunchTool?.(tool, '')
      }
    } catch (e: any) {
      alert(e?.message || `${tool} not installed`)
    }
  }

  // Auto-load databases when service is running
  useEffect(() => {
    dbInfos.forEach((db, idx) => {
      if (db.status?.status === 'running' && db.databases.length === 0 && !db.loadingDbs) {
        loadDatabases(idx, db.type)
      }
    })
  }, [dbInfos.map(d => d.status?.status).join(',')])

  async function loadDatabases(idx: number, dbType: string) {
    setDbInfos(prev => prev.map((db, i) => i === idx ? { ...db, loadingDbs: true } : db))
    try {
      const dbs = await window.go?.app?.App?.ListDatabases?.(dbType)
      setDbInfos(prev => prev.map((db, i) => i === idx ? { ...db, databases: dbs || [], loadingDbs: false } : db))
    } catch {
      setDbInfos(prev => prev.map((db, i) => i === idx ? { ...db, databases: [], loadingDbs: false } : db))
    }
  }

  async function openInHeidiSQL(dbType: string) {
    try {
      await window.go?.app?.App?.LaunchHeidiSQL?.(dbType)
    } catch (e: any) {
      alert(e?.message || 'HeidiSQL not installed')
    }
  }

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-lg font-medium">Databases</h2>
          <p className="text-xs text-text-muted mt-1">Manage database servers and browse databases</p>
        </div>
        <div className="flex items-center gap-2">
          {EXTERNAL_TOOLS.filter(t => installedTools[t.key]).map(t => {
            const Icon = t.icon
            return (
              <button
                key={t.key}
                onClick={() => launchTool(t.key)}
                className="flex items-center gap-2 px-3 py-1.5 bg-accent text-bg-primary rounded-lg text-xs font-medium hover:bg-accent-hover transition-colors"
                title={`Launch ${t.label}`}
              >
                <Icon size={12} />
                {t.label}
              </button>
            )
          })}
        </div>
      </div>

      <div className="space-y-4">
        {dbInfos.map((db, idx) => {
          const isRunning = db.status?.status === 'running'
          const port = db.status?.port || db.defaultPort
          const version = db.status?.version

          return (
            <div key={db.type} className="bg-bg-secondary rounded-lg border border-border">
              {/* Header */}
              <div className="flex items-center justify-between px-4 py-3 border-b border-border">
                <div className="flex items-center gap-3">
                  <div className={`w-2 h-2 rounded-full ${isRunning ? 'bg-status-green' : 'bg-status-red'}`} />
                  <div>
                    <span className="font-medium text-sm">{db.label}</span>
                    {version && <span className="text-xs text-text-dim ml-2">v{version}</span>}
                  </div>
                  <span className="text-xs text-text-muted font-mono">
                    {db.type}://{db.user}@127.0.0.1:{port}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  {isRunning && (
                    <button
                      onClick={() => loadDatabases(idx, db.type)}
                      className="p-1.5 text-text-dim hover:text-text-primary transition-colors"
                      title="Refresh databases"
                    >
                      <RefreshCw size={12} className={db.loadingDbs ? 'animate-spin' : ''} />
                    </button>
                  )}
                  <button
                    onClick={() => openInHeidiSQL(db.type)}
                    className="flex items-center gap-1.5 px-2.5 py-1 text-xs text-accent hover:text-accent-hover transition-colors"
                    title="Connect directly in HeidiSQL"
                  >
                    <ExternalLink size={11} />
                    Open in HeidiSQL
                  </button>
                </div>
              </div>

              {/* Database list */}
              <div className="px-4 py-3">
                {!isRunning ? (
                  <p className="text-xs text-text-dim">Service not running — start {db.label} to see databases</p>
                ) : db.loadingDbs ? (
                  <p className="text-xs text-text-dim">Loading databases...</p>
                ) : db.databases.length === 0 ? (
                  <p className="text-xs text-text-dim">No databases found</p>
                ) : (
                  <div className="flex flex-wrap gap-2">
                    {db.databases.map(name => (
                      <span
                        key={name}
                        className="inline-flex items-center px-2.5 py-1 bg-bg-primary border border-border rounded text-xs font-mono text-text-muted"
                      >
                        <Database size={10} className="mr-1.5 text-text-dim" />
                        {name}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
