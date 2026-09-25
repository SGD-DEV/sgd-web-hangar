import { useState, useEffect, useCallback } from 'react'
import { Download, RefreshCw } from 'lucide-react'
import { call, useAction } from '../../lib/api'
import { Button, Modal, Field, inputCls } from '../ui/Controls'
import type { Project } from './ProjectList'

// App projects: a Node / Python / ... program Hangar runs as a Windows
// service; the web server forwards the project's domain to its port.

interface AppStatus {
  service: string
  state: string
  detail: string
  log_path: string
  listening: boolean
}

const appPresets = [
  { label: 'Node (npm start)', command: 'npm start', build: 'npm ci' },
  { label: 'Node (build + start)', command: 'npm start', build: 'npm ci && npm run build' },
  { label: 'Node (server.js)', command: 'node server.js', build: '' },
  { label: 'Python (app.py)', command: 'python app.py', build: 'pip install -r requirements.txt' },
  { label: 'Python venv + uvicorn', command: '.venv\\Scripts\\python -m uvicorn main:app --host 127.0.0.1 --port %PORT%', build: '' },
]

export function AppDialog({ project, onClose, onChanged }: {
  project: Project
  onClose: () => void
  onChanged: (p: Project) => void
}) {
  const [cfg, setCfg] = useState({
    folder: project.path || '',
    command: project.app?.command || '',
    port: project.app?.port || 0,
    build_command: project.app?.build_command || '',
    envText: (project.app?.env || []).join('\n'),
  })
  const [saved, setSaved] = useState(!!project.app)
  const [port, setPort] = useState(project.app?.port || 0)
  const [status, setStatus] = useState<AppStatus | null>(null)
  const [log, setLog] = useState<string[]>([])
  const [output, setOutput] = useState('')
  const set = (patch: Partial<typeof cfg>) => setCfg(c => ({ ...c, ...patch }))
  const installed = !!status && status.state !== 'not-installed' && status.state !== 'unknown'

  const refresh = useCallback(async () => {
    try {
      setStatus(await call<AppStatus>('GetAppStatus', project.name))
      setLog(await call<string[]>('GetAppLog', project.name, 80) || [])
    } catch { /* shown on the next action */ }
  }, [project.name])

  useEffect(() => {
    refresh()
    const t = setInterval(refresh, 4000)
    return () => clearInterval(t)
  }, [refresh])

  useEffect(() => {
    if (!project.app) call<number>('SuggestAppPort').then(p => setCfg(c => c.port ? c : { ...c, port: p })).catch(() => {})
  }, [project.app])

  async function browse() {
    try { const dir = await call<string>('PickProjectDirectory'); if (dir) set({ folder: dir }) } catch { /* cancelled */ }
  }

  const [save, saving] = useAction(async () => {
    const p = await call<Project>('SaveProjectApp', project.name, cfg.folder, {
      command: cfg.command, port: Number(cfg.port) || 0, build_command: cfg.build_command,
      env: cfg.envText.split('\n'),
    })
    setSaved(true)
    setPort(p.app?.port || 0)
    onChanged(p)
    await refresh()
  }, { success: installed ? 'Saved - service restarted' : 'Saved', error: 'Could not save' })

  const [install, installing] = useAction(async () => {
    await call('InstallAppService', project.name)
    await refresh()
  }, { success: 'Service installed and started - it now starts with Windows', error: 'Could not install service' })

  const [uninstall, uninstalling] = useAction(async () => {
    await call('UninstallAppService', project.name)
    await refresh()
  }, { success: 'Service removed', error: 'Could not remove service' })

  const [control, controlling] = useAction(async (action: 'StartApp' | 'StopApp' | 'RestartApp') => {
    await call(action, project.name)
    await refresh()
  }, { error: 'Service action failed' })

  const [deploy, deploying] = useAction(async () => {
    setOutput('')
    try {
      setOutput(await call<string>('DeployApp', project.name))
    } finally {
      await refresh()
    }
  }, { success: 'Deployed', error: 'Deploy failed' })

  const running = status?.state === 'running'
  const stateColor = running ? (status?.listening ? 'bg-status-green' : 'bg-status-yellow') : status?.state === 'stopped' ? 'bg-status-red' : 'bg-text-dim'
  const stateText = !status ? '...'
    : status.state === 'not-installed' ? 'No service yet - save, then install the service'
    : running ? (status.listening ? `Running, answering on port ${port}` : `Running, but nothing answers on port ${port} yet - check the log`)
    : status.state
  const busy = saving || installing || uninstalling || controlling || deploying

  return (
    <Modal title={`App - ${project.name}`} onClose={onClose} width="max-w-2xl">
      <div className="space-y-4">
        <div className="flex items-center gap-2 text-xs bg-bg-primary border border-border rounded-lg px-3 py-2">
          <span className={`inline-block w-2 h-2 rounded-full ${stateColor}`} />
          <span className="text-text-muted">{stateText}</span>
          {status && <span className="ml-auto font-mono text-text-dim">{status.service}</span>}
        </div>

        <Field label="Folder" hint="The app's working directory. The service may write here (uploads, SQLite, caches) - and nowhere else.">
          <div className="flex gap-2">
            <input className={inputCls} value={cfg.folder} onChange={e => set({ folder: e.target.value })} />
            <Button onClick={browse}>Browse...</Button>
          </div>
        </Field>

        <div className="flex flex-wrap gap-1.5">
          {appPresets.map(p => (
            <button key={p.label} onClick={() => set({ command: p.command, build_command: p.build })}
              className="text-[11px] px-2 py-0.5 rounded border border-border text-text-muted hover:text-text-primary hover:border-text-dim">
              {p.label}
            </button>
          ))}
        </div>

        <div className="grid grid-cols-4 gap-3">
          <div className="col-span-3">
            <Field label="Start command" hint={<>Runs in <span className="font-mono">cmd.exe</span> in the folder. The app must listen on 127.0.0.1 and the port from <span className="font-mono">%PORT%</span>.</>}>
              <input className={inputCls} value={cfg.command} onChange={e => set({ command: e.target.value })} placeholder="npm start" />
            </Field>
          </div>
          <Field label="Port">
            <input className={inputCls} value={cfg.port || ''} onChange={e => set({ port: Number(e.target.value.replace(/\D/g, '')) })} />
          </Field>
        </div>

        <Field label="Build command (optional)" hint="Run by Deploy after git pull and before the restart, as your user.">
          <input className={inputCls} value={cfg.build_command} onChange={e => set({ build_command: e.target.value })} placeholder="npm ci && npm run build" />
        </Field>

        <Field label="Environment (optional)" hint="KEY=value per line. PORT, HOST, DB_* and MAIL_* come from the project's Database and Mail settings.">
          <textarea className={`${inputCls} h-16 resize-y`} value={cfg.envText} onChange={e => set({ envText: e.target.value })} placeholder="NODE_ENV=production" />
        </Field>

        <div className="flex flex-wrap justify-between gap-2 pt-2 border-t border-border">
          <div className="flex gap-2">
            {installed && <>
              <Button disabled={busy} onClick={() => control(running ? 'StopApp' : 'StartApp')}>{running ? 'Stop' : 'Start'}</Button>
              <Button disabled={busy} onClick={() => control('RestartApp')} icon={<RefreshCw size={12} />}>Restart</Button>
              <Button disabled={busy} busy={deploying} onClick={deploy} icon={<Download size={12} />} title="git pull (if a repository), build command, restart">Deploy</Button>
            </>}
          </div>
          <div className="flex gap-2">
            <Button variant={saved ? 'secondary' : 'primary'} busy={saving} disabled={busy || !cfg.command || !cfg.port} onClick={save}>Save</Button>
            {saved && status?.state === 'not-installed' && (
              <Button variant="primary" busy={installing} disabled={busy} onClick={install} title="Asks for administrator rights once">Install service</Button>
            )}
          </div>
        </div>

        {output && <pre className="max-h-40 overflow-auto bg-bg-primary border border-border rounded-lg p-2 text-[11px] font-mono text-text-dim whitespace-pre-wrap select-text">{output}</pre>}

        <div>
          <div className="flex items-center justify-between mb-1">
            <span className="text-xs text-text-muted">Log</span>
            {status && <span className="text-[11px] font-mono text-text-dim truncate ml-4" title={status.log_path}>{status.log_path}</span>}
          </div>
          <pre className="h-40 overflow-auto bg-bg-primary border border-border rounded-lg p-2 text-[11px] font-mono text-text-muted whitespace-pre-wrap select-text">
            {log.length ? log.join('\n') : 'No output yet.'}
          </pre>
        </div>

        {installed && (
          <div className="text-right">
            <Button variant="danger" disabled={busy} busy={uninstalling} onClick={() => {
              if (window.confirm(`Remove the Windows service ${status?.service}? The project and its files stay.`)) uninstall()
            }}>Remove service</Button>
          </div>
        )}
      </div>
    </Modal>
  )
}
