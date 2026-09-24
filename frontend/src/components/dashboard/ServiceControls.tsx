import { useState, useEffect } from 'react'
import { Play, Square, RotateCcw, Loader2, Skull } from 'lucide-react'
import { call, toast, errorMessage } from '../../lib/api'

interface ServiceStatus {
  name: string
  status: 'running' | 'stopped' | 'starting' | 'initializing' | 'stopping' | 'error'
  port: number
  version: string
  pid: number
  uptime: string
  started_at: string
  error?: string
}

interface ServiceControlsProps {
  serviceName: string
  status?: ServiceStatus
}

// ErrorPanel renders a service error and, when the message includes the
// "[port=X pid=Y name=Z]" tag emitted by services that detect a port-in-use
// conflict, also shows a "Kill <name> (PID Y)" button. After kill succeeds,
// onAfterKill() is called - typically the parent re-tries Start.
function ErrorPanel({ message, onAfterKill }: { message: string; onAfterKill: () => void }) {
  const [killing, setKilling] = useState(false)
  const [killError, setKillError] = useState('')

  // Match the "[port=80 pid=1234 name=httpd.exe]" suffix the Go side appends.
  const m = message.match(/\[port=(\d+)\s+pid=(\d+)\s+name=(\S+?)\]/)
  const owner = m ? { port: Number(m[1]), pid: Number(m[2]), name: m[3] } : null
  const cleanMessage = owner ? message.replace(m![0], '').trim() : message

  async function killAndRetry() {
    if (!owner) return
    setKilling(true)
    setKillError('')
    try {
      // @ts-ignore Wails bindings
      await window.go?.app?.App?.KillProcessByPID?.(owner.pid)
      // Tiny delay so the OS releases the socket before we retry.
      await new Promise(r => setTimeout(r, 500))
      onAfterKill()
    } catch (e: any) {
      setKillError(e?.message || String(e))
    } finally {
      setKilling(false)
    }
  }

  return (
    <div className="bg-status-red/10 border border-status-red/20 rounded-lg p-4 space-y-3">
      <div>
        <p className="text-xs text-status-red font-medium mb-1">Error</p>
        <p className="text-xs text-status-red font-mono whitespace-pre-wrap">{cleanMessage}</p>
      </div>
      {owner && (
        <div className="flex items-center justify-between bg-bg-primary/50 rounded p-2">
          <p className="text-xs text-text-muted">
            Port <span className="font-mono text-text-primary">{owner.port}</span> is held by{' '}
            <span className="font-mono text-text-primary">{owner.name}</span>{' '}
            <span className="text-text-dim">(PID {owner.pid})</span>
          </p>
          <button
            onClick={killAndRetry}
            disabled={killing}
            className="flex items-center gap-1.5 px-2.5 py-1 bg-status-red/20 text-status-red border border-status-red/30 rounded text-xs font-medium hover:bg-status-red/30 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {killing ? <Loader2 size={11} className="animate-spin" /> : <Skull size={11} />}
            {killing ? 'Killing...' : `Kill ${owner.name} & retry`}
          </button>
        </div>
      )}
      {killError && (
        <p className="text-xs text-status-red font-mono">{killError}</p>
      )}
    </div>
  )
}

const serviceLabels: Record<string, string> = {
  apache: 'Apache HTTP Server',
  nginx: 'Nginx Web Server',
  mysql: 'MySQL Database',
  postgresql: 'PostgreSQL Database',
  mailpit: 'Mailpit Mail Server',
}

export default function ServiceControls({ serviceName, status }: ServiceControlsProps) {
  const label = serviceLabels[serviceName] || serviceName
  const [sslEnabled, setSslEnabled] = useState(false)
  // Optimistic "I just clicked Start, give me feedback now" state. The backend
  // will set StatusText=starting eventually, but the UI polls every 3 seconds
  // so without this the button feels frozen until the next poll lands.
  const [localPending, setLocalPending] = useState<'start' | 'stop' | 'restart' | null>(null)
  const [actionError, setActionError] = useState<string>('')

  useEffect(() => {
    loadConfig()
  }, [])

  // Clear local pending state once the backend status catches up.
  useEffect(() => {
    if (!localPending) return
    if (localPending === 'start' && (status?.status === 'running' || status?.status === 'error')) setLocalPending(null)
    if (localPending === 'stop' && status?.status === 'stopped') setLocalPending(null)
    if (localPending === 'restart' && status?.status === 'running') setLocalPending(null)
  }, [status?.status, localPending])

  async function loadConfig() {
    try {
      const cfg = await window.go?.app?.App?.GetConfig?.()
      if (cfg) setSslEnabled(!!cfg.ssl_enabled)
    } catch (e) { /* ignore */ }
  }

  async function handleStart() {
    setLocalPending('start')
    setActionError('')
    try {
      await call('StartService', serviceName)
      toast.success(isWebServer ? `${label} is now serving all sites` : `${label} started`)
    } catch (e) {
      // Surface the Go error message right next to the button so the user
      // doesn't have to dig into the Logs tab to figure out what went wrong.
      setActionError(errorMessage(e))
      toast.error(`${label} did not start`, errorMessage(e))
      setLocalPending(null)
    }
  }

  async function handleStop() {
    setLocalPending('stop')
    setActionError('')
    try {
      await call('StopService', serviceName)
      toast.success(`${label} stopped`, isWebServer ? 'All sites are offline until a web server runs again.' : undefined)
    } catch (e) {
      setActionError(errorMessage(e))
      toast.error(`Could not stop ${label}`, errorMessage(e))
      setLocalPending(null)
    }
  }

  async function handleRestart() {
    setLocalPending('restart')
    setActionError('')
    try {
      await call('RestartService', serviceName)
      toast.success(`${label} restarted`)
    } catch (e) {
      setActionError(errorMessage(e))
      toast.error(`Could not restart ${label}`, errorMessage(e))
      setLocalPending(null)
    }
  }

  const isRunning = status?.status === 'running'
  const isInitializing = status?.status === 'initializing'
  const isStarting = status?.status === 'starting' || isInitializing || localPending === 'start'
  const isStopping = localPending === 'stop'
  const isBusy = isStarting || isStopping || localPending !== null
  const isWebServer = serviceName === 'apache' || serviceName === 'nginx'
  const isMailpit = serviceName === 'mailpit'
  const portDisplay = status?.port
    ? (isMailpit ? `SMTP: 1025, UI: ${status.port}` : isWebServer && sslEnabled ? `${status.port}, 443` : String(status.port))
    : '—'

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-medium">{label}</h2>
        <p className="text-xs text-text-muted mt-1">Service overview and controls</p>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <div className="bg-bg-secondary rounded-lg p-4 border border-border">
          <p className="text-xs text-text-muted mb-1">Status</p>
          <div className="flex items-center gap-2">
            <div className={`w-2 h-2 rounded-full ${isStarting ? 'bg-status-yellow animate-pulse' : isRunning ? 'bg-status-green' : 'bg-status-red'}`} />
            <span className="text-sm font-medium capitalize">
              {isStarting && <Loader2 size={12} className="inline-block mr-1 animate-spin" />}
              {status?.status || 'Unknown'}
            </span>
          </div>
        </div>
        <div className="bg-bg-secondary rounded-lg p-4 border border-border">
          <p className="text-xs text-text-muted mb-1">Port{isWebServer && sslEnabled ? 's' : ''}</p>
          <span className="text-sm font-mono">{portDisplay}</span>
        </div>
        <div className="bg-bg-secondary rounded-lg p-4 border border-border">
          <p className="text-xs text-text-muted mb-1">Uptime</p>
          <span className="text-sm font-mono">{status?.uptime || '—'}</span>
        </div>
      </div>

      {status?.version && (
        <div className="bg-bg-secondary rounded-lg p-4 border border-border">
          <p className="text-xs text-text-muted mb-1">Version</p>
          <span className="text-sm font-mono">{status.version}</span>
        </div>
      )}

      {(status?.error || actionError) && (
        <ErrorPanel
          message={actionError || status?.error || ''}
          onAfterKill={() => { setActionError(''); handleStart() }}
        />
      )}

      <div className="flex items-center gap-3">
        {isStarting ? (
          <button
            disabled
            className="flex items-center gap-2 px-4 py-2 bg-status-yellow/10 text-status-yellow border border-status-yellow/20 rounded-lg text-sm font-medium cursor-not-allowed"
          >
            <Loader2 size={14} className="animate-spin" />
            {isInitializing
              ? 'Initializing first-run data... (30-90s)'
              : 'Starting...'}
          </button>
        ) : isStopping ? (
          <button
            disabled
            className="flex items-center gap-2 px-4 py-2 bg-status-yellow/10 text-status-yellow border border-status-yellow/20 rounded-lg text-sm font-medium cursor-not-allowed"
          >
            <Loader2 size={14} className="animate-spin" />
            Stopping...
          </button>
        ) : !isRunning ? (
          <button
            onClick={handleStart}
            className="flex items-center gap-2 px-4 py-2 bg-accent text-bg-primary rounded-lg text-sm font-medium hover:bg-accent-hover transition-colors"
          >
            <Play size={14} />
            Start
          </button>
        ) : (
          <button
            onClick={handleStop}
            className="flex items-center gap-2 px-4 py-2 bg-status-red/10 text-status-red border border-status-red/20 rounded-lg text-sm font-medium hover:bg-status-red/20 transition-colors"
          >
            <Square size={14} />
            Stop
          </button>
        )}
        <button
          onClick={handleRestart}
          disabled={!isRunning || isBusy}
          className="flex items-center gap-2 px-4 py-2 bg-bg-secondary text-text-muted border border-border rounded-lg text-sm font-medium hover:text-text-primary hover:border-text-dim transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
        >
          <RotateCcw size={14} />
          Restart
        </button>
      </div>
    </div>
  )
}
