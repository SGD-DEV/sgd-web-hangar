import { useState, useEffect, useCallback } from 'react'
import { Database, RefreshCw, Box, Layers, Code, Plus, Download, Trash2, Copy, FolderOpen, Globe, Table2, HardDrive as Elephant } from 'lucide-react'
import { call, toast, useAction, errorMessage } from '../../lib/api'
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
      toast.error(`Could not list ${t} databases`, errorMessage(e))
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
  }, { error: 'Could not open phpMyAdmin' })
  const [openAdminer, openingAdminer] = useAction(async () => {
    const url = await call<string>('OpenAdminer')
    await call('OpenURL', url)
  }, { error: 'Could not open Adminer' })
  const [openPgAdmin, openingPgAdmin] = useAction(() => call('OpenPgAdmin'),
    { success: 'pgAdmin is starting (first start takes a few seconds)', error: 'Could not open pgAdmin' })
  const [launchTool] = useAction((t: ToolKey) => t === 'heidisql' ? call('OpenHeidiSQL') : call('LaunchTool', t, ''),
    { error: 'Could not launch tool' })

  const [backup, backingUp] = useAction(async (t: DBType, name: string) => {
    const path = await call<string>('BackupDatabase', t, name)
    loadBackups()
    return path
  }, { success: p => `Backup written: ${p.split('\\').pop()}`, error: 'Backup failed' })

  const [drop] = useAction(async (t: DBType, name: string) => {
    const path = await call<string>('DropDatabase', t, name)
    await loadDatabases(t)
    loadBackups()
    return path
  }, { success: p => `Database deleted. A backup was saved first: ${p.split('\\').pop()}`, error: 'Could not delete database' })

  function confirmDrop(t: DBType, name: string) {
    if (window.confirm(`Delete database "${name}"?\n\nHangar saves a backup to the backups folder first.`)) drop(t, name)
  }

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="mb-6">
        <h2 className="text-lg font-medium">Databases</h2>
        <p className="text-xs text-text-muted mt-1">Create databases for your sites, back them up, and open them in a management tool.</p>
      </div>

      <Card className="p-4 mb-4">
        <p className="text-xs text-text-muted mb-3">Web tools (open in the browser, only reachable from this machine, log in automatically)</p>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="primary" busy={openingPMA} disabled={!mysqlRunning} onClick={openPMA} icon={<Table2 size={12} />}
            title={mysqlRunning ? 'phpMyAdmin for MySQL' : 'Start MySQL first'}>phpMyAdmin</Button>
          <Button variant="primary" busy={openingAdminer} onClick={openAdminer} icon={<Globe size={12} />}
            title="Adminer: one page for MySQL and PostgreSQL">Adminer</Button>
          <Button variant="primary" busy={openingPgAdmin} onClick={openPgAdmin} icon={<Elephant size={12} />}
            title="pgAdmin 4 (desktop app shipped with PostgreSQL)">pgAdmin</Button>
          {DESKTOP_TOOLS.filter(t => installedTools[t.key]).map(t => {
            const Icon = t.icon
            return <Button key={t.key} onClick={() => launchTool(t.key)} icon={<Icon size={12} />}>{t.label}</Button>
          })}
        </div>
        <p className="text-[11px] text-text-dim mt-3">
          Adminer login: system <span className="font-mono">MySQL</span>, server <span className="font-mono">127.0.0.1</span>, user <span className="font-mono">root</span>, empty password
          (PostgreSQL: user <span className="font-mono">postgres</span>).
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
                    <button onClick={() => loadDatabases(db.type)} className="p-1.5 text-text-dim hover:text-text-primary" title="Refresh">
                      <RefreshCw size={12} className={loading[db.type] ? 'animate-spin' : ''} />
                    </button>
                  )}
                  <Button size="xs" variant="primary" disabled={!isRunning} onClick={() => setCreating(db.type)} icon={<Plus size={11} />}>
                    New database
                  </Button>
                </div>
              </div>
              <div className="px-4 py-3">
                {!isRunning ? (
                  <p className="text-xs text-text-dim">Not running - start {db.label} on the Servers page.</p>
                ) : list.length === 0 ? (
                  <p className="text-xs text-text-dim">{loading[db.type] ? 'Loading...' : 'No databases yet.'}</p>
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
                              <Button size="xs" variant="ghost" busy={backingUp} onClick={() => backup(db.type, name)} icon={<Download size={11} />} title="Dump to the backups folder">Backup</Button>
                              <Button size="xs" variant="ghost" onClick={() => confirmDrop(db.type, name)} icon={<Trash2 size={11} />} title="Delete (after an automatic backup)">Delete</Button>
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
                onClick={() => call('OpenInExplorer', backups[0].path.replace(/\\[^\\]+\\[^\\]+$/, '')).catch(e => toast.error('Open failed', errorMessage(e)))}>
                Open folder
              </Button>
            )}
          </div>
          <div className="px-4 py-3">
            {backups.length === 0 ? <p className="text-xs text-text-dim">No backups yet.</p> : (
              <div className="divide-y divide-border">
                {backups.slice(0, 15).map(b => (
                  <div key={b.path} className="flex items-center justify-between py-1.5 text-xs">
                    <span className="font-mono text-text-primary">{b.file}</span>
                    <span className="text-text-dim">{b.type} · {(b.size / 1024).toFixed(0)} KB · {new Date(b.mod_time).toLocaleString()}</span>
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
  }, { success: `Database ${name} created`, error: 'Could not create database' })

  return (
    <Modal title={`New ${type === 'mysql' ? 'MySQL' : 'PostgreSQL'} database`} onClose={onClose} width="max-w-md">
      <div className="space-y-3">
        <Field label="Database name" hint="Letters, digits and _ (e.g. wordpress_blog)">
          <input className={inputCls} value={name} onChange={e => setName(e.target.value)} autoFocus />
        </Field>
        <Field label="User" hint="Leave empty to use the database name. The user gets full rights on this database only.">
          <input className={inputCls} value={user} onChange={e => setUser(e.target.value)} placeholder={name || 'same as database'} />
        </Field>
        <Field label="Password" hint="Leave empty to generate a strong one.">
          <input className={inputCls} value={password} onChange={e => setPassword(e.target.value)} placeholder="generated" />
        </Field>
        <div className="flex justify-end gap-2 pt-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" busy={busy} disabled={!name.trim()} onClick={create}>Create</Button>
        </div>
      </div>
    </Modal>
  )
}

function CredentialsModal({ c, onClose }: { c: Credentials; onClose: () => void }) {
  const rows: [string, string][] = [
    ['Host', c.host], ['Port', String(c.port)], ['Database', c.database], ['User', c.user], ['Password', c.password],
  ]
  const text = rows.map(([k, v]) => `${k}: ${v}`).join('\n')
  return (
    <Modal title="Database ready" onClose={onClose} width="max-w-md">
      <p className="text-xs text-text-muted mb-3">Paste these into your site's installer (e.g. WordPress wp-config / Laravel .env). The password is shown only now.</p>
      <div className="bg-bg-primary border border-border rounded-lg divide-y divide-border">
        {rows.map(([k, v]) => (
          <div key={k} className="flex items-center justify-between px-3 py-2">
            <span className="text-xs text-text-muted w-20">{k}</span>
            <span className="text-xs font-mono flex-1 select-text">{v}</span>
            <button onClick={() => navigator.clipboard.writeText(v).then(() => toast.info(`${k} copied`))} className="text-text-dim hover:text-text-primary" title="Copy"><Copy size={11} /></button>
          </div>
        ))}
      </div>
      <div className="flex justify-end gap-2 pt-4">
        <Button icon={<Copy size={12} />} onClick={() => navigator.clipboard.writeText(text).then(() => toast.info('All details copied'))}>Copy all</Button>
        <Button variant="primary" onClick={onClose}>Done</Button>
      </div>
    </Modal>
  )
}
