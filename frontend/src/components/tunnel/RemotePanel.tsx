import { ReactNode, useCallback, useEffect, useState } from 'react'
import { Plus, Trash2, Save, Wand2, List, ScrollText } from 'lucide-react'
import { call, useAction, errorMessage, confirmDialog } from '../../lib/api'
import { Button, Card, Field, StatusDot, inputCls } from '../ui/Controls'

// Tunnel created in the Cloudflare dashboard (the service runs it from a
// token). Hangar edits its routes and DNS records through the Cloudflare API.

interface Rule {
  hostname: string
  path: string
  service: string
  no_tls_verify: boolean
  from_project?: string
}

interface RemoteTunnel {
  id: string
  name: string
  status: string
  connections: number
  created_at: string
}

interface DNSRecord {
  zone_id: string
  zone_name: string
  id: string
  type: string
  name: string
  content: string
}

interface RemoteState {
  tunnel: RemoteTunnel
  routes: Rule[] | null
  target: string
  missing_dns: string[] | null
  foreign_dns: DNSRecord[] | null
  other_tunnels: RemoteTunnel[] | null
}

interface SyncReport {
  changes: string[] | null
  errors: string[] | null
}

const tunnelStatus: Record<string, string> = { healthy: 'verbunden', degraded: 'eingeschränkt', down: 'getrennt', inactive: 'nie verbunden' }

// Local services that are usually published besides the projects.
const servicePresets = [
  { label: 'Filebrowser', service: 'http://127.0.0.1:8081' },
  { label: 'SYNDOC', service: 'http://127.0.0.1:3000' },
  { label: 'Machine Controller', service: 'http://127.0.0.1:8765' },
]

export default function RemotePanel({ apiToken, onChanged, log }: { apiToken: boolean; onChanged: () => void; log: ReactNode }) {
  const [state, setState] = useState<RemoteState | null>(null)
  const [loadError, setLoadError] = useState('')
  const [report, setReport] = useState<SyncReport | null>(null)
  const [editToken, setEditToken] = useState(false)
  const [tab, setTab] = useState<'routes' | 'log'>('routes')

  const load = useCallback(async () => {
    if (!apiToken) return
    try {
      setState(await call<RemoteState>('GetTunnelRemoteState'))
      setLoadError('')
    } catch (e) {
      setLoadError(errorMessage(e))
    }
  }, [apiToken])

  useEffect(() => { load() }, [load])

  const showReport = (r: SyncReport | undefined) => {
    if (!r) return
    setReport(r)
    load()
  }

  const [sync, syncing] = useAction(async () => {
    const r = await call<SyncReport>('SyncTunnelRemote')
    showReport(r)
    return r
  }, {
    success: r => (r.changes?.length ? `${r.changes.length} Änderung(en) bei Cloudflare übernommen` : 'Alles aktuell - nichts zu ändern'),
    error: 'Abgleich fehlgeschlagen',
  })

  if (!apiToken || editToken) {
    return <ApiTokenCard hasToken={apiToken} onCancel={apiToken ? () => setEditToken(false) : undefined}
      onSaved={r => { setEditToken(false); onChanged(); showReport(r) }} />
  }

  const missing = new Set(state?.missing_dns || [])

  return (
    <>
      <Card className="p-4 mt-6">
        <div className="flex items-center gap-4">
          <div className="flex-1">
            <p className="text-sm font-medium flex items-center gap-2">
              <StatusDot state={state?.tunnel.status === 'healthy' ? 'ok' : state ? 'warn' : 'off'} />
              {state
                ? <>Tunnel „{state.tunnel.name}“ - {tunnelStatus[state.tunnel.status] || state.tunnel.status}{state.tunnel.connections > 0 && ` (${state.tunnel.connections} Verbindungen)`}</>
                : loadError ? 'Cloudflare nicht erreichbar' : 'Lade Tunnel…'}
            </p>
            <p className="text-xs text-text-muted mt-1">
              Öffentliche Domains, die du bei einem Projekt einträgst, veröffentlicht Hangar beim Speichern automatisch: Route im Tunnel und DNS-Eintrag bei Cloudflare.
            </p>
          </div>
          <Button onClick={sync} busy={syncing} icon={<Wand2 size={12} />} title="Routen und DNS-Einträge mit den Projekt-Domains abgleichen">Jetzt abgleichen</Button>
          <Button size="xs" onClick={() => setEditToken(true)}>API-Token ändern</Button>
        </div>
        {loadError && <p className="text-xs text-status-red mt-3 whitespace-pre-wrap">{loadError}</p>}
        {report && ((report.changes?.length || 0) + (report.errors?.length || 0) > 0) && (
          <div className="mt-3 pt-3 border-t border-border space-y-0.5">
            {(report.changes || []).map((c, i) => <p key={i} className="text-xs text-status-green">✓ {c}</p>)}
            {(report.errors || []).map((c, i) => <p key={i} className="text-xs text-status-red">✗ {c}</p>)}
          </div>
        )}
      </Card>

      <div className="flex items-center gap-1 border-b border-border mt-6 mb-4">
        {([['routes', 'Routen', List], ['log', 'Log', ScrollText]] as const).map(([id, label, Icon]) => (
          <button key={id} onClick={() => setTab(id)}
            className={`flex items-center gap-1.5 px-3 py-2 text-xs font-medium -mb-px border-b-2 transition-colors ${tab === id ? 'border-accent text-text-primary' : 'border-transparent text-text-muted hover:text-text-primary'}`}>
            <Icon size={12} /> {label}
          </button>
        ))}
      </div>

      {tab === 'routes' && state && (
        <>
          <RemoteRoutes routes={state.routes || []} missing={missing} onReport={showReport} />
          <Leftovers state={state} onChanged={load} />
        </>
      )}
      {tab === 'log' && log}
    </>
  )
}

function ApiTokenCard({ hasToken, onSaved, onCancel }: { hasToken: boolean; onSaved: (r?: SyncReport) => void; onCancel?: () => void }) {
  const [token, setToken] = useState('')
  const [save, saving] = useAction(async () => {
    const r = await call<SyncReport>('SetCloudflareAPIToken', token.trim())
    setToken('')
    onSaved(r)
    return r
  }, { success: 'API-Token gespeichert - Projekt-Domains sind veröffentlicht', error: 'API-Token nicht gespeichert' })

  return (
    <Card className="p-5 mt-6">
      <h3 className="text-sm font-medium mb-1">{hasToken ? 'API-Token ändern' : 'Cloudflare-API-Token hinterlegen'}</h3>
      <p className="text-xs text-text-muted mb-3">
        Dieser Tunnel wurde im Cloudflare-Dashboard angelegt. Mit einem API-Token trägt Hangar Routen und DNS-Einträge selbst ein - du musst das Dashboard dafür nicht mehr öffnen.
      </p>
      <ol className="text-xs text-text-muted list-decimal ml-4 space-y-0.5 mb-4">
        <li>Cloudflare → Profil (oben rechts) → <b>API-Tokens</b> → <b>Token erstellen</b> → <b>Benutzerdefiniertes Token</b></li>
        <li>Berechtigungen: <b>Konto → Cloudflare Tunnel → Bearbeiten</b> und <b>Zone → DNS → Bearbeiten</b></li>
        <li>Zonenressourcen: <b>Alle Zonen einschließen</b> → Token erstellen und hier einfügen</li>
      </ol>
      <div className="flex items-end gap-2">
        <Field label="API-Token">
          <input type="password" autoComplete="off" className={`${inputCls} w-96`} value={token}
            onChange={e => setToken(e.target.value)} onKeyDown={e => { if (e.key === 'Enter' && token.trim()) save() }} />
        </Field>
        <Button variant="primary" busy={saving} disabled={!token.trim()} onClick={save} icon={<Save size={12} />}>Prüfen & speichern</Button>
        {onCancel && <Button onClick={onCancel}>Abbrechen</Button>}
      </div>
      <p className="text-[11px] text-text-dim mt-2">Gespeichert wird er neben dem Tunnel-Token im geschützten Ordner (secrets).</p>
    </Card>
  )
}

function RemoteRoutes({ routes, missing, onReport }: { routes: Rule[]; missing: Set<string>; onReport: (r?: SyncReport) => void }) {
  const [host, setHost] = useState('')
  const [service, setService] = useState(servicePresets[0].service)

  const [apply, applying] = useAction(async (next: Rule[]) => {
    const r = await call<SyncReport>('SaveTunnelRemoteRoutes', next)
    onReport(r)
    return r
  }, { success: 'Tunnel aktualisiert', error: 'Tunnel konnte nicht aktualisiert werden' })

  const add = async () => {
    const h = host.trim().toLowerCase()
    if (!h) return
    const r = await apply([...routes, { hostname: h, path: '', service: service.trim(), no_tls_verify: false }])
    if (r) setHost('')
  }

  const remove = async (r: Rule) => {
    const ok = await confirmDialog({
      title: 'Route entfernen?',
      message: `${r.hostname} ist danach nicht mehr erreichbar. Der DNS-Eintrag wird ebenfalls gelöscht.`,
      confirmLabel: 'Entfernen',
      danger: true,
    })
    if (ok) await apply(routes.filter(x => x !== r))
  }

  const isPreset = (s: string) => servicePresets.some(p => p.service === s)

  return (
    <Card>
      <div className="grid grid-cols-[1.5fr_1.2fr_1fr_auto] gap-2 px-4 py-2 border-b border-border text-[11px] text-text-dim uppercase tracking-wider">
        <span>Öffentliche Adresse</span><span>Lokales Ziel</span><span>Gehört zu</span><span />
      </div>
      {routes.length === 0 && <p className="px-4 py-4 text-xs text-text-dim">Noch keine Routen. Trage bei einem Projekt unter „Bearbeiten“ öffentliche Domains ein, oder lege unten eine Route für einen anderen Dienst an.</p>}
      {routes.map((r, i) => (
        <div key={i} className="grid grid-cols-[1.5fr_1.2fr_1fr_auto] gap-2 px-4 py-2 items-center border-b border-border text-xs">
          <div className="min-w-0">
            <button className="font-mono text-accent hover:underline truncate" onClick={() => call('OpenURL', `https://${r.hostname}`).catch(() => {})}>
              {r.hostname}{r.path}
            </button>
            {missing.has(r.hostname) && <p className="text-[10px] text-status-yellow">DNS-Eintrag fehlt - „Jetzt abgleichen“</p>}
          </div>
          <span className="font-mono text-text-muted truncate">{r.service}</span>
          <span className="text-text-muted truncate">
            {r.from_project ? <>Projekt <b>{r.from_project}</b></> : servicePresets.find(p => p.service === r.service)?.label || 'manuell'}
          </span>
          {r.from_project
            ? <span className="p-1.5 text-text-dim" title="Kommt aus dem Projekt - dort unter „Bearbeiten“ entfernen"><Trash2 size={12} className="opacity-30" /></span>
            : <button onClick={() => remove(r)} disabled={applying} className="p-1.5 text-text-dim hover:text-status-red" title="Route entfernen"><Trash2 size={12} /></button>}
        </div>
      ))}
      <div className="px-4 py-3 flex items-end gap-2 flex-wrap">
        <Field label="Weitere Adresse">
          <input className={`${inputCls} w-64`} value={host} placeholder="files.deinedomain.de" onChange={e => setHost(e.target.value)} />
        </Field>
        <Field label="Ziel">
          <div className="flex gap-2">
            <select className={`${inputCls} w-44`} value={isPreset(service) ? service : ''} onChange={e => setService(e.target.value || 'http://127.0.0.1:')}>
              {servicePresets.map(p => <option key={p.service} value={p.service}>{p.label}</option>)}
              <option value="">Anderer Port…</option>
            </select>
            <input className={`${inputCls} w-52 font-mono`} value={service} onChange={e => setService(e.target.value)} />
          </div>
        </Field>
        <Button variant="primary" busy={applying} disabled={!host.trim()} onClick={add} icon={<Plus size={12} />}>Veröffentlichen</Button>
      </div>
      <p className="px-4 pb-3 text-[11px] text-status-yellow">
        Nur Dienste mit eigenem Login veröffentlichen (Filebrowser, SYNDOC, Machine Controller) und dort starke Passwörter verwenden. Datenbank-Tools, Mailpit und dieses Panel nie.
      </p>
    </Card>
  )
}

function Leftovers({ state, onChanged }: { state: RemoteState; onChanged: () => void }) {
  const dns = state.foreign_dns || []
  const tunnels = state.other_tunnels || []

  const [delDNSNow, deletingDNS] = useAction(async (r: DNSRecord) => {
    await call('DeleteTunnelDNSRecord', r.zone_id, r.id)
    onChanged()
  }, { success: 'DNS-Eintrag gelöscht', error: 'DNS-Eintrag nicht gelöscht' })
  const delDNS = async (r: DNSRecord) => {
    const ok = await confirmDialog({
      title: 'DNS-Eintrag löschen?',
      message: `${r.name} zeigt auf einen anderen Tunnel. Nach dem Löschen ist die Adresse nicht mehr erreichbar, bis du sie hier veröffentlichst.`,
      confirmLabel: 'Löschen',
      danger: true,
    })
    if (ok) await delDNSNow(r)
  }

  const [delTunnelNow, deletingTunnel] = useAction(async (t: RemoteTunnel) => {
    await call('DeleteRemoteTunnel', t.id)
    onChanged()
  }, { success: 'Tunnel gelöscht', error: 'Tunnel nicht gelöscht' })
  const delTunnel = async (t: RemoteTunnel) => {
    const ok = await confirmDialog({
      title: `Tunnel „${t.name}“ löschen?`,
      message: 'Der Tunnel wird in deinem Cloudflare-Konto gelöscht. Ein Server, der ihn noch benutzt, ist danach nicht mehr erreichbar.',
      confirmLabel: 'Tunnel löschen',
      danger: true,
    })
    if (ok) await delTunnelNow(t)
  }

  if (dns.length === 0 && tunnels.length === 0) return null
  return (
    <Card className="p-4 mt-6">
      <h3 className="text-sm font-medium mb-1">Alte Einträge</h3>
      <p className="text-xs text-text-muted mb-3">Überbleibsel anderer Tunnel in deinem Cloudflare-Konto, z. B. von einem früheren Server.</p>
      {dns.map(r => (
        <div key={r.id} className="flex items-center justify-between px-3 py-2 bg-bg-primary rounded border border-border mb-1 text-xs">
          <span><span className="font-mono">{r.name}</span> <span className="text-text-dim">→ Tunnel {tunnelName(tunnels, r.content)}</span></span>
          <Button size="xs" variant="danger" busy={deletingDNS} onClick={() => delDNS(r)} icon={<Trash2 size={11} />}>DNS löschen</Button>
        </div>
      ))}
      {tunnels.map(t => (
        <div key={t.id} className="flex items-center justify-between px-3 py-2 bg-bg-primary rounded border border-border mb-1 text-xs">
          <span>Tunnel <b>{t.name}</b> <span className="text-text-dim">- {tunnelStatus[t.status] || t.status}{t.connections > 0 && `, ${t.connections} Verbindungen aktiv`}</span></span>
          <Button size="xs" variant="danger" busy={deletingTunnel} onClick={() => delTunnel(t)} icon={<Trash2 size={11} />}>Tunnel löschen</Button>
        </div>
      ))}
    </Card>
  )
}

function tunnelName(tunnels: RemoteTunnel[], content: string) {
  const id = content.toLowerCase().replace('.cfargotunnel.com', '')
  return tunnels.find(t => t.id === id)?.name || '(unbekannt)'
}
