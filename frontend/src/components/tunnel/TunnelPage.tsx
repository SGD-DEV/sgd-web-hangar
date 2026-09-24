import { useCallback, useEffect, useState } from 'react'
import { Cloud, RefreshCw, Plus, Trash2, Save, RotateCcw, LogIn, Globe, Wand2, FileCode, List, ScrollText, CheckCircle2, Square, Download, ExternalLink } from 'lucide-react'
import { call, toast, useAction, errorMessage } from '../../lib/api'
import { Button, Card, Field, StatusDot, inputCls } from '../ui/Controls'

interface Info {
  config_path: string
  config_exists: boolean
  dir: string
  cloudflared_path: string
  version: string
  logged_in: boolean
  service_name: string
  service_state: string
  service_detail?: string
}

interface Rule {
  hostname: string
  path: string
  service: string
  no_tls_verify: boolean
  from_project?: string
}

interface TunnelConfig {
  tunnel: string
  credentials_file: string
  ingress: Rule[]
}

interface TunnelEntry {
  id: string
  name: string
  created_at: string
  connections: number
}

type Tab = 'rules' | 'yaml' | 'log'

export default function TunnelPage() {
  const [info, setInfo] = useState<Info | null>(null)
  const [cfg, setCfg] = useState<TunnelConfig | null>(null)
  const [dirty, setDirty] = useState(false)
  const [tab, setTab] = useState<Tab>('rules')
  const [origin, setOrigin] = useState('http://127.0.0.1:80')

  const loadInfo = useCallback(async () => {
    try { setInfo(await call<Info>('GetTunnelInfo')) } catch (e) { toast.error('Tunnel status', errorMessage(e)) }
  }, [])

  const loadConfig = useCallback(async () => {
    try {
      const c = await call<TunnelConfig>('GetTunnelConfig')
      setCfg({ ...c, ingress: (c.ingress || []).filter(r => r.hostname || r.path) })
      setDirty(false)
    } catch (e) {
      toast.error('Could not read config.yml', errorMessage(e))
    }
  }, [])

  useEffect(() => {
    loadInfo()
    loadConfig()
    call<string>('WebServerOrigin').then(setOrigin).catch(() => {})
    const t = setInterval(loadInfo, 10000)
    return () => clearInterval(t)
  }, [loadInfo, loadConfig])

  const [save, saving] = useAction(async () => {
    if (!cfg) return
    await call('SaveTunnelConfig', cfg)
    await loadConfig()
  }, { success: 'config.yml saved - restart the tunnel to apply it', error: 'Could not save config' })

  const [saveAndRestart, savingRestart] = useAction(async () => {
    if (!cfg) return
    if (dirty) await call('SaveTunnelConfig', cfg)
    await call('RestartTunnel')
    await loadConfig()
    await loadInfo()
  }, { success: 'Tunnel restarted with the new configuration', error: 'Could not apply configuration' })

  const [sync, syncing] = useAction(async () => {
    const c = await call<TunnelConfig>('SyncTunnelFromProjects')
    setCfg({ ...c, ingress: (c.ingress || []).filter(r => r.hostname || r.path) })
    setDirty(true)
    return c.ingress?.length || 0
  }, { success: 'Rules updated from project domains - review and save', error: 'Sync failed' })

  const setupNeeded = info && (!info.config_exists || !cfg?.tunnel)

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-lg font-medium flex items-center gap-2"><Cloud size={16} /> Cloudflare Tunnel</h2>
          <p className="text-xs text-text-muted mt-1">
            Publish sites on your own domains without opening router ports. Visitors connect to Cloudflare, which forwards through the tunnel to this machine.
          </p>
        </div>
        <Button onClick={() => { loadInfo(); loadConfig() }} icon={<RefreshCw size={12} />}>Refresh</Button>
      </div>

      {info && <StatusCard info={info} cfg={cfg} onChanged={() => { loadInfo(); loadConfig() }} />}

      {info && setupNeeded && <SetupWizard info={info} onDone={() => { loadInfo(); loadConfig() }} />}

      {cfg && !setupNeeded && (
        <>
          <div className="flex items-center gap-1 border-b border-border mt-6 mb-4">
            {([['rules', 'Routes', List], ['yaml', 'config.yml', FileCode], ['log', 'Log', ScrollText]] as const).map(([id, label, Icon]) => (
              <button key={id} onClick={() => setTab(id)}
                className={`flex items-center gap-1.5 px-3 py-2 text-xs font-medium -mb-px border-b-2 transition-colors ${tab === id ? 'border-accent text-text-primary' : 'border-transparent text-text-muted hover:text-text-primary'}`}>
                <Icon size={12} /> {label}
              </button>
            ))}
          </div>

          {tab === 'rules' && (
            <RulesEditor
              cfg={cfg}
              origin={origin}
              dirty={dirty}
              onChange={c => { setCfg(c); setDirty(true) }}
              actions={
                <>
                  <Button onClick={sync} busy={syncing} icon={<Wand2 size={12} />} title="Add a route for every public domain of your projects and drop routes whose project is gone">
                    Sync from projects
                  </Button>
                  <Button onClick={save} busy={saving} disabled={!dirty} icon={<Save size={12} />}>Save</Button>
                  <Button variant="primary" onClick={saveAndRestart} busy={savingRestart}
                    disabled={info?.service_state === 'not-installed'} icon={<RotateCcw size={12} />}>
                    {dirty ? 'Save & apply' : 'Restart tunnel'}
                  </Button>
                </>
              }
            />
          )}
          {tab === 'yaml' && <YamlEditor onSaved={loadConfig} />}
          {tab === 'log' && <TunnelLog />}
        </>
      )}
    </div>
  )
}

function StatusCard({ info, cfg, onChanged }: { info: Info; cfg: TunnelConfig | null; onChanged: () => void }) {
  const state = info.service_state
  const [install, installing] = useAction(async () => { await call('InstallTunnelService'); onChanged() },
    { success: 'Tunnel service installed and started', error: 'Service installation failed' })
  const [restart, restarting] = useAction(async () => { await call('RestartTunnel'); onChanged() },
    { success: 'Tunnel restarted', error: 'Could not restart tunnel' })
  const [stop, stopping] = useAction(async () => { await call('StopTunnel'); onChanged() },
    { success: 'Tunnel stopped - published sites are offline', error: 'Could not stop tunnel' })

  const dot = state === 'running' ? 'ok' : state === 'starting' || state === 'stopping' ? 'busy' : state === 'not-installed' ? 'warn' : 'off'
  const label: Record<string, string> = {
    running: 'Running', stopped: 'Stopped', starting: 'Starting...', stopping: 'Stopping...',
    'not-installed': 'Service not installed', unknown: 'Unknown',
  }

  return (
    <Card className="p-4">
      <div className="grid grid-cols-4 gap-4">
        <div>
          <p className="text-xs text-text-muted mb-1">Service ({info.service_name})</p>
          <div className="flex items-center gap-2 text-sm font-medium"><StatusDot state={dot} /> {label[state] || state}</div>
          {info.service_detail && <p className="text-[11px] text-text-dim mt-1">{info.service_detail}</p>}
        </div>
        <div>
          <p className="text-xs text-text-muted mb-1">Tunnel</p>
          <p className="text-xs font-mono truncate" title={cfg?.tunnel}>{cfg?.tunnel || '-'}</p>
        </div>
        <div>
          <p className="text-xs text-text-muted mb-1">cloudflared</p>
          <p className="text-xs font-mono">{info.version || (info.cloudflared_path ? '?' : 'not installed')}</p>
        </div>
        <div>
          <p className="text-xs text-text-muted mb-1">Cloudflare account</p>
          <p className="text-xs">{info.logged_in ? 'Logged in' : 'Not logged in'}</p>
        </div>
      </div>
      <div className="flex items-center gap-2 mt-4 pt-4 border-t border-border">
        {state === 'not-installed' ? (
          <Button variant="primary" busy={installing} disabled={!cfg?.tunnel} onClick={install} icon={<Download size={12} />}
            title="Registers cloudflared as a Windows service that starts with the computer (asks for administrator rights once)">
            Install as Windows service
          </Button>
        ) : (
          <>
            <Button variant="primary" busy={restarting} onClick={restart} icon={<RotateCcw size={12} />}>
              {state === 'running' ? 'Restart' : 'Start'}
            </Button>
            {state === 'running' && <Button variant="danger" busy={stopping} onClick={stop} icon={<Square size={12} />}>Stop</Button>}
            <Button busy={installing} onClick={install} title="Reinstall the service (e.g. after moving the config)">Reinstall service</Button>
          </>
        )}
        <span className="text-[11px] text-text-dim font-mono ml-auto truncate" title={info.config_path}>{info.config_path}</span>
      </div>
    </Card>
  )
}

function SetupWizard({ info, onDone }: { info: Info; onDone: () => void }) {
  const [name, setName] = useState('hangar')
  const [tunnels, setTunnels] = useState<TunnelEntry[] | null>(null)

  const [login, loggingIn] = useAction(async () => { await call('TunnelLogin'); onDone() },
    { success: 'Logged in to Cloudflare', error: 'Login failed' })
  const [list, listing] = useAction(async () => setTunnels(await call<TunnelEntry[]>('ListTunnels') || []),
    { error: 'Could not list tunnels' })
  const [create, creating] = useAction(async () => { await call('CreateTunnel', name); onDone() },
    { success: 'Tunnel created and written to config.yml', error: 'Could not create tunnel' })
  const [use, using] = useAction(async (id: string) => { await call('UseExistingTunnel', id); onDone() },
    { success: 'Tunnel selected', error: 'Could not use tunnel' })

  const step = (n: number, done: boolean, title: string, body: React.ReactNode) => (
    <div className="flex gap-3">
      <div className={`w-6 h-6 rounded-full flex-shrink-0 flex items-center justify-center text-xs font-medium ${done ? 'bg-status-green/20 text-status-green' : 'bg-bg-primary border border-border text-text-muted'}`}>
        {done ? <CheckCircle2 size={14} /> : n}
      </div>
      <div className="flex-1 pb-5">
        <p className="text-sm font-medium mb-1">{title}</p>
        {body}
      </div>
    </div>
  )

  return (
    <Card className="p-5 mt-6">
      <h3 className="text-sm font-medium mb-4">Set up the tunnel</h3>
      {step(1, !!info.cloudflared_path, 'Install cloudflared', info.cloudflared_path
        ? <p className="text-xs text-text-dim font-mono">{info.cloudflared_path}</p>
        : <p className="text-xs text-text-muted">Install <b>cloudflared</b> on the Packages page (Tools), then refresh.</p>)}
      {step(2, info.logged_in, 'Log in to Cloudflare', info.logged_in
        ? <p className="text-xs text-text-dim">Account certificate found.</p>
        : <>
          <p className="text-xs text-text-muted mb-2">Opens the browser. Log in and pick the domain (zone) you want to use. This page waits until you are done.</p>
          <Button variant="primary" busy={loggingIn} disabled={!info.cloudflared_path} onClick={login} icon={<LogIn size={12} />}>
            {loggingIn ? 'Waiting for the browser...' : 'Log in with Cloudflare'}
          </Button>
        </>)}
      {step(3, false, 'Create or pick a tunnel', <>
        <div className="flex items-end gap-2">
          <Field label="New tunnel name">
            <input className={`${inputCls} w-56`} value={name} onChange={e => setName(e.target.value)} />
          </Field>
          <Button variant="primary" busy={creating} disabled={!info.logged_in || !name} onClick={create} icon={<Plus size={12} />}>Create tunnel</Button>
          <Button busy={listing} disabled={!info.logged_in} onClick={list}>Use existing...</Button>
        </div>
        {tunnels && (
          <div className="mt-3 space-y-1">
            {tunnels.length === 0 && <p className="text-xs text-text-dim">No tunnels in this account yet.</p>}
            {tunnels.map(t => (
              <div key={t.id} className="flex items-center justify-between px-3 py-2 bg-bg-primary rounded border border-border">
                <div>
                  <span className="text-sm">{t.name}</span>
                  <span className="text-[11px] text-text-dim font-mono ml-2">{t.id}</span>
                  {t.connections > 0 && <span className="text-[11px] text-status-yellow ml-2">(connected elsewhere)</span>}
                </div>
                <Button size="xs" busy={using} onClick={() => use(t.id)}>Use</Button>
              </div>
            ))}
            <p className="text-[11px] text-text-dim">Using an existing tunnel needs its credentials file (&lt;id&gt;.json) in %USERPROFILE%\.cloudflared.</p>
          </div>
        )}
      </>)}
      {step(4, false, 'Install the Windows service', <p className="text-xs text-text-muted">After the tunnel exists, use "Install as Windows service" above. The tunnel then runs on boot, independent of Hangar.</p>)}
    </Card>
  )
}

function RulesEditor({ cfg, origin, dirty, onChange, actions }: {
  cfg: TunnelConfig
  origin: string
  dirty: boolean
  onChange: (c: TunnelConfig) => void
  actions: React.ReactNode
}) {
  const rules = cfg.ingress
  const update = (i: number, patch: Partial<Rule>) =>
    onChange({ ...cfg, ingress: rules.map((r, j) => j === i ? { ...r, ...patch } : r) })
  const remove = (i: number) => onChange({ ...cfg, ingress: rules.filter((_, j) => j !== i) })
  const add = () => onChange({ ...cfg, ingress: [...rules, { hostname: '', path: '', service: origin, no_tls_verify: false }] })

  const [routeDNS, routing] = useAction(async (host: string) => {
    const out = await call<string>('RouteTunnelDNS', host)
    return out
  }, { success: out => out || 'DNS record created', error: 'Could not create DNS record' })

  return (
    <div>
      <div className="flex items-center justify-between mb-3">
        <p className="text-xs text-text-muted">
          Each route sends a public hostname to a local service. Project sites go to the web server (<span className="font-mono">{origin}</span>);
          other apps can point at their own port, e.g. Filebrowser at <span className="font-mono">http://127.0.0.1:8081</span>.
          Anything else gets a 404.
        </p>
      </div>
      <Card>
        <div className="grid grid-cols-[1.4fr_1.4fr_auto_auto] gap-2 px-4 py-2 border-b border-border text-[11px] text-text-dim uppercase tracking-wider">
          <span>Public hostname</span><span>Local service</span><span title="Skip certificate check (https origins with self-signed certs)">No TLS verify</span><span />
        </div>
        {rules.length === 0 && <p className="px-4 py-4 text-xs text-text-dim">No routes yet. Add public domains to a project (Edit) and click "Sync from projects", or add a route by hand.</p>}
        {rules.map((r, i) => (
          <div key={i} className="grid grid-cols-[1.4fr_1.4fr_auto_auto] gap-2 px-4 py-2 items-center border-b border-border last:border-b-0">
            <div>
              <input className={inputCls} value={r.hostname} placeholder="www.example.com" onChange={e => update(i, { hostname: e.target.value })} />
              {r.from_project && <p className="text-[10px] text-text-dim mt-0.5">project: {r.from_project}</p>}
            </div>
            <input className={inputCls} value={r.service} placeholder="http://127.0.0.1:80" onChange={e => update(i, { service: e.target.value })} />
            <input type="checkbox" className="accent-accent justify-self-center" checked={r.no_tls_verify} onChange={e => update(i, { no_tls_verify: e.target.checked })} />
            <div className="flex items-center gap-1">
              <Button size="xs" busy={routing} disabled={!r.hostname || dirty} onClick={() => routeDNS(r.hostname)}
                icon={<Globe size={11} />} title={dirty ? 'Save first' : 'Create/update the CNAME record in Cloudflare DNS for this hostname'}>
                DNS
              </Button>
              {r.hostname && (
                <button onClick={() => call('OpenURL', `https://${r.hostname}`).catch(e => toast.error('Open failed', errorMessage(e)))}
                  className="p-1.5 text-text-dim hover:text-text-primary" title="Open public URL"><ExternalLink size={12} /></button>
              )}
              <button onClick={() => remove(i)} className="p-1.5 text-text-dim hover:text-status-red" title="Remove route"><Trash2 size={12} /></button>
            </div>
          </div>
        ))}
        <div className="px-4 py-2 border-t border-border flex items-center justify-between">
          <Button size="xs" onClick={add} icon={<Plus size={11} />}>Add route</Button>
          <span className="text-[11px] text-text-dim">Catch-all: <span className="font-mono">http_status:404</span> (added automatically)</span>
        </div>
      </Card>
      <div className="flex items-center gap-2 mt-4">
        {actions}
        {dirty && <span className="text-xs text-status-yellow ml-2">Unsaved changes</span>}
      </div>
    </div>
  )
}

function YamlEditor({ onSaved }: { onSaved: () => void }) {
  const [raw, setRaw] = useState('')
  const [loaded, setLoaded] = useState(false)
  const [status, setStatus] = useState<{ ok: boolean; msg: string } | null>(null)

  useEffect(() => {
    call<string>('GetTunnelConfigRaw').then(r => { setRaw(r || ''); setLoaded(true) })
      .catch(e => toast.error('Could not read config.yml', errorMessage(e)))
  }, [])

  const [validate, validating] = useAction(async () => {
    try {
      await call('ValidateTunnelConfig', raw)
      setStatus({ ok: true, msg: 'Valid' })
    } catch (e) {
      setStatus({ ok: false, msg: errorMessage(e) })
    }
  })
  const [save, saving] = useAction(async () => {
    await call('SaveTunnelConfigRaw', raw)
    setStatus({ ok: true, msg: 'Saved (previous version kept as config.yml.bak)' })
    onSaved()
  }, { success: 'config.yml saved - restart the tunnel to apply it', error: 'Not saved' })

  if (!loaded) return null
  return (
    <div>
      <textarea
        className={`${inputCls} h-96 text-xs leading-relaxed resize-y`}
        value={raw}
        spellCheck={false}
        onChange={e => { setRaw(e.target.value); setStatus(null) }}
      />
      <div className="flex items-center gap-2 mt-3">
        <Button onClick={validate} busy={validating} icon={<CheckCircle2 size={12} />}>Validate</Button>
        <Button variant="primary" onClick={save} busy={saving} icon={<Save size={12} />}>Save</Button>
        {status && <span className={`text-xs whitespace-pre-wrap ${status.ok ? 'text-status-green' : 'text-status-red'}`}>{status.msg}</span>}
      </div>
      <p className="text-[11px] text-text-dim mt-2">
        Saving validates the file with <span className="font-mono">cloudflared tunnel ingress validate</span> first. Reference:{' '}
        <button className="text-accent hover:underline" onClick={() => call('OpenURL', 'https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/configure-tunnels/local-management/configuration-file/')}>configuration file docs</button>
      </p>
    </div>
  )
}

function TunnelLog() {
  const [lines, setLines] = useState<string[]>([])
  const load = useCallback(() => {
    call<string[]>('GetTunnelLog', 300).then(l => setLines(l || [])).catch(e => toast.error('Log', errorMessage(e)))
  }, [])
  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load])
  return (
    <Card className="p-3">
      <pre className="text-[11px] font-mono text-text-muted whitespace-pre-wrap max-h-[480px] overflow-y-auto select-text">
        {lines.length ? lines.join('\n') : 'No log yet - the service writes here once it runs.'}
      </pre>
    </Card>
  )
}
