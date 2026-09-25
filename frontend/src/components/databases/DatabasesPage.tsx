import { useState, useEffect, useCallback } from 'react'
import { Database, RefreshCw, Box, Layers, Code, Plus, Download, Trash2, Copy, FolderOpen, Globe, Table2, HardDrive as Elephant } from 'lucide-react'
import { call, toast, useAction, errorMessage, confirmDialog } from '../../lib/api'
import { Button, Card, Field, Modal, StatusDot, inputCls } from '../ui/Controls'

interface ServiceStatus {
  status: string
  port: number
  version: string
}

type DBType = 'mysql' | 'postgresql'

interface Credentials {
  type: string
  host: string
  port: number
  database: string
  user: string
  password: string
}

interface Backup {
  type: string
  file: string
  path: string
  size: number
  mod_time: string
}

const DBS: { type: DBType; label: string; admin: string; system: string[] }[] = [
  { type: 'mysql', label: 'MySQL', admin: 'root', system: ['mysql', 'information_schema', 'performance_schema', 'sys'] },
  { type: 'postgresql', label: 'PostgreSQL', admin: 'postgres', system: ['postgres'] },
]

type ToolKey = 'heidisql' | 'dbeaver' | 'pocketbase' | 'vscode'
const DESKTOP_TOOLS: { key: ToolKey; label: string; icon: any }[] = [
  { key: 'heidisql', label: 'HeidiSQL', icon: Database },
  { key: 'dbeaver', label: 'DBeaver', icon: Layers },
  { key: 'pocketbase', label: 'PocketBase', icon: Box },
  { key: 'vscode', label: 'VS Code', icon: Code },
]

export default function DatabasesPage({ services }: { services: Record<string, ServiceStatus> }) {
  const [dbs, setDbs] = useState<Record<DBType, string[]>>({ mysql: [], postgresql: [] })
  const [loading, setLoading] = useState<Record<DBType, boolean>>({ mysql: false, postgresql: false })
  const [installedTools, setInstalledTools] = useState<Record<ToolKey, boolean>>({ heidisql: false, dbeaver: false, pocketbase: false, vscode: false })
  const [creating, setCreating] = useState<DBType | null>(null)
  const [created, setCreated] = useState<Credentials | null>(null)
  const [backups, setBackups] = useState<Backup[]>([])

  const running = (t: DBType) => services[t]?.status === 'running'

  const loadDatabases = useCallback(async (t: DBType) => {
    setLoading(l => ({ ...l, [t]: true }))
    try {
      setDbs(d => ({ ...d, [t]: [] }))
      const list = await call<string[]>('ListDatabases', t)
      setDbs(d => ({ ...d, [t]: list || [] }))
    } catch (e) {
      toast.error(`${t}-Datenbanken konnten nicht geladen werden`, errorMessage(e))
    } finally {
      setLoading(l => ({ ...l, [t]: false }))
    }
  }, [])

  const loadBackups = useCallback(() => {
    call<Backup[]>('ListBackups').then(b => setBackups(b || [])).catch(() => {})
  }, [])

  const mysqlRunning = running('mysql')
  const pgRunning = running('postgresql')
  useEffect(() => { if (mysqlRunning) loadDatabases('mysql') }, [mysqlRunning, loadDatabases])
  useEffect(() => { if (pgRunning) loadDatabases('postgresql') }, [pgRunning, loadDatabases])
  useEffect(loadBackups, [loadBackups])

  useEffect(() => {
    ;(async () => {
      const r: Record<ToolKey, boolean> = { heidisql: false, dbeaver: false, pocketbase: false, vscode: false }
      r.heidisql = !!(await call<boolean>('IsHeidiSQLInstalled').catch(() => false))
      for (const t of ['dbeaver', 'pocketbase', 'vscode'] as ToolKey[]) {
        r[t] = !!(await call<boolean>('IsToolInstalled', t).catch(() => false))
      }
      setInstalledTools(r)
    })()
  }, [])

  const [openPMA, openingPMA] = useAction(async () => {
    const url = await call<string>('OpenPhpMyAdmin')
    await call('OpenURL', url)
  }, { error: 'phpMyAdmin konnte nicht geöffnet werden' })
  const [openAdminer, openingAdminer] = useAction(async () => {
    const url = await call<string>('OpenAdminer')
    await call('OpenURL', url)
  }, { error: 'Adminer konnte nicht geöffnet werden' })
  const [openAdminerPg, openingAdminerPg] = useAction(async () => {
    const url = await call<string>('OpenAdminer')
    await call('OpenURL', (url.endsWith('/') ? url : url + '/') + '?hangar=pgsql')
  }, { error: 'Adminer konnte nicht geöffnet werden' })
  const [openPgAdmin, openingPgAdmin] = useAction(() => call('OpenPgAdmin'),
    { success: 'pgAdmin startet (der erste Start dauert ein paar Sekunden)', error: 'pgAdmin konnte nicht geöffnet werden' })
  const [launchTool] = useAction((t: ToolKey) => t === 'heidisql' ? call('OpenHeidiSQL') : call('LaunchTool', t, ''),
    { error: 'Programm konnte nicht gestartet werden' })

  const [backup, backingUp] = useAction(async (t: DBType, name: string) => {
    const path = await call<string>('BackupDatabase', t, name)
    loadBackups()
    return path
  }, { success: p => `Backup gespeichert: ${p.split('\\').pop()}`, error: 'Backup fehlgeschlagen' })

  const [drop] = useAction(async (t: DBType, name: string) => {
    const path = await call<string>('DropDatabase', t, name)
    await loadDatabases(t)
    loadBackups()
    return path
  }, { success: p => `Datenbank gelöscht. Vorher wurde ein Backup gespeichert: ${p.split('\\').pop()}`, error: 'Datenbank konnte nicht gelöscht werden' })

  async function confirmDrop(t: DBType, name: string) {
    if (await confirmDialog({
      title: `Datenbank ${name} löschen?`,
      message: `Hangar speichert vorher ein Backup im Backup-Ordner.\nDer gleichnamige Benutzer wird mit entfernt, außer er hat Rechte auf eine andere Datenbank.`,
      confirmLabel: 'Datenbank löschen',
      danger: true,
    })) drop(t, name)
  }

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="mb-6">
        <h2 className="text-lg font-medium">Datenbanken</h2>
        <p className="text-xs text-text-muted mt-1">Datenbanken für deine Seiten anlegen, sichern und in einem Verwaltungsprogramm öffnen.</p>
      </div>

      <Card className="p-4 mb-4">
        <p className="text-xs text-text-muted mb-3">Web-Tools (öffnen im Browser, nur von diesem Rechner erreichbar, melden sich automatisch an)</p>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="primary" busy={openingPMA} disabled={!mysqlRunning} onClick={openPMA} icon={<Table2 size={12} />}
            title={mysqlRunning ? 'phpMyAdmin für MySQL' : 'Starte zuerst MySQL'}>phpMyAdmin</Button>
          <Button variant="primary" busy={openingAdminer} onClick={openAdminer} icon={<Globe size={12} />}
            title="Adminer für MySQL (meldet sich automatisch an)">Adminer</Button>
          <Button variant="primary" busy={openingAdminerPg} disabled={!pgRunning} onClick={openAdminerPg} icon={<Globe size={12} />}
            title={pgRunning ? 'Adminer für PostgreSQL (meldet sich automatisch an)' : 'Starte zuerst PostgreSQL'}>Adminer (PostgreSQL)</Button>
          <Button variant="primary" busy={openingPgAdmin} onClick={openPgAdmin} icon={<Elephant size={12} />}
            title="pgAdmin 4 (Desktop-Programm, wird mit PostgreSQL geliefert)">pgAdmin</Button>
          {DESKTOP_TOOLS.filter(t => installedTools[t.key]).map(t => {
            const Icon = t.icon
            return <Button key={t.key} onClick={() => launchTool(t.key)} icon={<Icon size={12} />}>{t.label}</Button>
          })}
        </div>
        <p className="text-[11px] text-text-dim mt-3">
          Adminer meldet sich automatisch an. Falls doch ein Login erscheint: Server <span className="font-mono">127.0.0.1</span>, Benutzer <span className="font-mono">root</span>
          (PostgreSQL: <span className="font-mono">postgres</span>), Passwort leer.
        </p>
      </Card>

      <div className="space-y-4">
        {DBS.map(db => {
          const st = services[db.type]
          const isRunning = running(db.type)
          const list = dbs[db.type]
          return (
            <Card key={db.type}>
              <div className="flex items-center justify-between px-4 py-3 border-b border-border">
                <div className="flex items-center gap-3">
                  <StatusDot state={isRunning ? 'ok' : 'off'} />
                  <span className="font-medium text-sm">{db.label}</span>
                  {st?.version && <span className="text-xs text-text-dim">v{st.version}</span>}
                  <span className="text-xs text-text-muted font-mono select-text">{db.admin}@127.0.0.1:{st?.port || (db.type === 'mysql' ? 3306 : 5432)}</span>
                </div>
                <div className="flex items-center gap-2">
                  {isRunning && (
                    <button onClick={() => loadDatabases(db.type)} className="p-1.5 text-text-dim hover:text-text-primary" title="Aktualisieren">
                      <RefreshCw size={12} className={loading[db.type] ? 'animate-spin' : ''} />
                    </button>
                  )}
                  <Button size="xs" variant="primary" disabled={!isRunning} onClick={() => setCreating(db.type)} icon={<Plus size={11} />}>
                    Neue Datenbank
                  </Button>
                </div>
              </div>
              <div className="px-4 py-3">
                {!isRunning ? (
                  <p className="text-xs text-text-dim">Läuft nicht - starte {db.label} auf der Seite Server.</p>
                ) : list.length === 0 ? (
                  <p className="text-xs text-text-dim">{loading[db.type] ? 'Lädt…' : 'Noch keine Datenbanken.'}</p>
                ) : (
                  <div className="divide-y divide-border">
                    {list.map(name => {
                      const isSystem = db.system.includes(name)
                      return (
                        <div key={name} className="flex items-center justify-between py-1.5">
                          <span className={`text-xs font-mono flex items-center gap-2 ${isSystem ? 'text-text-dim' : 'text-text-primary'}`}>
                            <Database size={11} className="text-text-dim" />{name}
                            {isSystem && <span className="text-[10px]">(system)</span>}
                          </span>
                          {!isSystem && (
                            <div className="flex items-center gap-1">
                              <Button size="xs" variant="ghost" busy={backingUp} onClick={() => backup(db.type, name)} icon={<Download size={11} />} title="In den Backup-Ordner sichern">Backup</Button>
                              <Button size="xs" variant="ghost" onClick={() => confirmDrop(db.type, name)} icon={<Trash2 size={11} />} title="Löschen (nach automatischem Backup)">Löschen</Button>
                            </div>
                          )}
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>
            </Card>
          )
        })}

        <Card>
          <div className="flex items-center justify-between px-4 py-3 border-b border-border">
            <span className="font-medium text-sm">Backups</span>
            {backups[0] && (
              <Button size="xs" icon={<FolderOpen size={11} />}
                onClick={() => call('OpenInExplorer', backups[0].path.replace(/\\[^\\]+\\[^\\]+$/, '')).catch(e => toast.error('Öffnen fehlgeschlagen', errorMessage(e)))}>
                Ordner öffnen
              </Button>
            )}
          </div>
          <div className="px-4 py-3">
            {backups.length === 0 ? <p className="text-xs text-text-dim">Noch keine Backups.</p> : (
              <div className="divide-y divide-border">
                {backups.slice(0, 15).map(b => (
                  <div key={b.path} className="flex items-center justify-between py-1.5 text-xs">
                    <span className="font-mono text-text-primary">{b.file}</span>
                    <span className="text-text-dim">{b.type} · {(b.size / 1024).toFixed(0)} KB · {new Date(b.mod_time).toLocaleString('de-DE')}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </Card>
      </div>

      {creating && (
        <CreateDatabase
          type={creating}
          onClose={() => setCreating(null)}
          onCreated={c => { setCreating(null); setCreated(c); loadDatabases(c.type as DBType) }}
        />
      )}
      {created && <CredentialsModal c={created} onClose={() => setCreated(null)} />}
    </div>
  )
}

function CreateDatabase({ type, onClose, onCreated }: { type: DBType; onClose: () => void; onCreated: (c: Credentials) => void }) {
  const [name, setName] = useState('')
  const [user, setUser] = useState('')
  const [password, setPassword] = useState('')
  const [create, busy] = useAction(async () => {
    const c = await call<Credentials>('CreateDatabase', type, name.trim(), user.trim(), password)
    onCreated(c)
  }, { success: `Datenbank ${name} angelegt`, error: 'Datenbank konnte nicht angelegt werden' })

  return (
    <Modal title={`Neue ${type === 'mysql' ? 'MySQL' : 'PostgreSQL'}-Datenbank`} onClose={onClose} width="max-w-md">
      <div className="space-y-3">
        <Field label="Datenbankname" hint="Buchstaben, Ziffern und _ (z. B. wordpress_blog)">
          <input className={inputCls} value={name} onChange={e => setName(e.target.value)} autoFocus />
        </Field>
        <Field label="Benutzer" hint="Leer lassen, um den Datenbanknamen zu verwenden. Der Benutzer bekommt volle Rechte nur auf diese Datenbank.">
          <input className={inputCls} value={user} onChange={e => setUser(e.target.value)} placeholder={name || 'wie die Datenbank'} />
        </Field>
        <Field label="Passwort" hint="Leer lassen, um ein sicheres zu erzeugen.">
          <input className={inputCls} value={password} onChange={e => setPassword(e.target.value)} placeholder="wird erzeugt" />
        </Field>
        <div className="flex justify-end gap-2 pt-2">
          <Button onClick={onClose}>Abbrechen</Button>
          <Button variant="primary" busy={busy} disabled={!name.trim()} onClick={create}>Anlegen</Button>
        </div>
      </div>
    </Modal>
  )
}

function CredentialsModal({ c, onClose }: { c: Credentials; onClose: () => void }) {
  const rows: [string, string][] = [
    ['Host', c.host], ['Port', String(c.port)], ['Datenbank', c.database], ['Benutzer', c.user], ['Passwort', c.password],
  ]
  const text = rows.map(([k, v]) => `${k}: ${v}`).join('\n')
  return (
    <Modal title="Datenbank bereit" onClose={onClose} width="max-w-md">
      <p className="text-xs text-text-muted mb-3">Trage diese Daten in den Installer deiner Seite ein (z. B. WordPress wp-config / Laravel .env). Das Passwort wird nur jetzt angezeigt.</p>
      <div className="bg-bg-primary border border-border rounded-lg divide-y divide-border">
        {rows.map(([k, v]) => (
          <div key={k} className="flex items-center justify-between px-3 py-2">
            <span className="text-xs text-text-muted w-20">{k}</span>
            <span className="text-xs font-mono flex-1 select-text">{v}</span>
            <button onClick={() => navigator.clipboard.writeText(v).then(() => toast.info(`${k} kopiert`))} className="text-text-dim hover:text-text-primary" title="Kopieren"><Copy size={11} /></button>
          </div>
        ))}
      </div>
      <div className="flex justify-end gap-2 pt-4">
        <Button icon={<Copy size={12} />} onClick={() => navigator.clipboard.writeText(text).then(() => toast.info('Alle Daten kopiert'))}>Alles kopieren</Button>
        <Button variant="primary" onClick={onClose}>Fertig</Button>
      </div>
    </Modal>
  )
}
