import { useState, useEffect, useCallback } from 'react'
import { FolderOpen, Plus, Trash2, Search, X, Loader2, Lock, Unlock, Pencil, Globe, Home, ShieldCheck, Copy, Database, Mail, GitBranch, Play } from 'lucide-react'
import { call, toast, useAction, errorMessage } from '../../lib/api'
import { Button, Modal, Field, inputCls } from '../ui/Controls'
import { DatabaseDialog, MailDialog, GitDialog } from './ProjectTools'
import { AppDialog } from './AppDialog'

export interface Project {
  name: string
  path: string
  domain: string
  aliases?: string[]
  php_version: string
  web_server: string
  ssl_enabled: boolean
  framework: string
  document_root: string
  proxy_target?: string
  local_only?: boolean
  database?: ProjectDatabase
  mail?: ProjectMail
  app?: { command: string; port: number; build_command?: string; env?: string[] }
}

export interface ProjectDatabase {
  type: string
  host: string
  port: number
  name: string
  user: string
  password: string
}

export interface ProjectMail {
  host: string
  port: number
  user: string
  password: string
  encryption: string
  from_email: string
  from_name: string
}

interface PHPVersion {
  version: string
  is_active: boolean
}

function localURL(p: Project) {
  return `${p.ssl_enabled ? 'https' : 'http'}://${p.domain}`
}

async function openURL(url: string) {
  try {
    await call('OpenURL', url)
  } catch (e) {
    toast.error('Browser konnte nicht geöffnet werden', errorMessage(e))
  }
}

export default function ProjectList() {
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [editing, setEditing] = useState<Project | null>(null)
  const [tool, setTool] = useState<{ kind: 'db' | 'mail' | 'git' | 'app'; project: Project } | null>(null)
  const [projectsRoot, setProjectsRoot] = useState('')
  const [domainSuffix, setDomainSuffix] = useState('.test')
  const [phpVersions, setPhpVersions] = useState<PHPVersion[]>([])
  const [webServer, setWebServer] = useState('apache')

  const loadProjects = useCallback(async () => {
    try {
      const p = await call<Project[]>('GetProjects')
      setProjects((p || []).sort((a, b) => a.name.localeCompare(b.name)))
    } catch (e) {
      toast.error('Projekte konnten nicht geladen werden', errorMessage(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadProjects()
    call<string>('GetProjectsRoot').then(r => r && setProjectsRoot(r)).catch(() => {})
    call<{ domain_suffix?: string }>('GetConfig').then(c => c?.domain_suffix && setDomainSuffix(c.domain_suffix)).catch(() => {})
    call<PHPVersion[]>('GetInstalledPHPVersions').then(v => setPhpVersions(v || [])).catch(() => {})
    call<string>('GetActiveWebServer').then(setWebServer).catch(() => {})
  }, [loadProjects])

  const [scan, scanning] = useAction(async () => {
    const found = await call<Project[]>('ScanProjects')
    await loadProjects()
    return found?.length || 0
  }, { success: n => n ? `${n} neue${n === 1 ? 's Projekt' : ' Projekte'} gefunden` : 'Keine neuen Projekte gefunden', error: 'Scan fehlgeschlagen' })

  const [toggleSSL] = useAction(async (p: Project) => {
    await call('UpdateProjectSettings', p.name, settingsOf(p, { ssl_enabled: !p.ssl_enabled }))
    await loadProjects()
    return !p.ssl_enabled
  }, { success: on => on ? 'HTTPS eingeschaltet' : 'HTTPS ausgeschaltet', error: 'HTTPS konnte nicht geändert werden' })

  const [changePHP] = useAction(async (p: Project, version: string) => {
    await call('UpdateProjectSettings', p.name, settingsOf(p, { php_version: version }))
    await loadProjects()
    return version
  }, { success: v => `Jetzt wird PHP ${v} verwendet`, error: 'PHP-Version konnte nicht gewechselt werden' })

  const [removing, setRemoving] = useState<Project | null>(null)

  const [openFolder] = useAction((p: Project) => call('OpenInExplorer', p.path), { error: 'Ordner konnte nicht geöffnet werden' })

  function confirmRemove(p: Project) {
    setRemoving(p)
  }

  const frameworkBadge: Record<string, string> = {
    laravel: 'text-status-red',
    wordpress: 'text-blue-400',
    symfony: 'text-status-yellow',
    proxy: 'text-accent',
    php: 'text-text-muted',
    unknown: 'text-text-dim',
  }

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-lg font-medium">Projekte</h2>
          <p className="text-xs text-text-muted mt-1">
            Seiten, die <span className="text-text-primary capitalize">{webServer}</span> ausliefert, aus{' '}
            <span className="font-mono text-text-primary">{projectsRoot || '...'}</span>
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button onClick={scan} busy={scanning} icon={<Search size={12} />} title="Ordner im Projektordner registrieren, die noch keine Projekte sind">
            Suchen
          </Button>
          <Button variant="primary" onClick={() => setShowCreate(true)} icon={<Plus size={12} />}>
            Neues Projekt
          </Button>
        </div>
      </div>

      {showCreate && (
        <CreateProject
          projectsRoot={projectsRoot}
          domainSuffix={domainSuffix}
          onClose={() => setShowCreate(false)}
          onCreated={async (name, detail, openApp) => {
            setShowCreate(false)
            await loadProjects()
            toast.success(`Projekt ${name} angelegt`, detail || 'Über das Stift-Symbol kannst du eine öffentliche Domain für den Cloudflare Tunnel hinzufügen.')
            if (openApp) {
              const all = await call<Project[]>('GetProjects')
              const me = all.find(x => x.name === name)
              if (me) setTool({ kind: 'app', project: me })
            }
          }}
        />
      )}

      {tool?.kind === 'db' && (
        <DatabaseDialog project={tool.project} onClose={() => setTool(null)} onChanged={() => loadProjects()} />
      )}
      {tool?.kind === 'mail' && (
        <MailDialog project={tool.project} onClose={() => setTool(null)} onChanged={() => loadProjects()} />
      )}
      {tool?.kind === 'app' && (
        <AppDialog project={tool.project} onClose={() => setTool(null)} onChanged={() => loadProjects()} />
      )}
      {tool?.kind === 'git' && (
        <GitDialog project={tool.project} onClose={() => setTool(null)} />
      )}

      {removing && (
        <RemoveProjectDialog
          project={removing}
          projectsRoot={projectsRoot}
          onClose={() => setRemoving(null)}
          onRemoved={async () => { setRemoving(null); await loadProjects() }}
        />
      )}

      {editing && (
        <EditProject
          project={editing}
          phpVersions={phpVersions}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null)
            await loadProjects()
          }}
        />
      )}

      {loading ? (
        <div className="text-text-dim text-sm flex items-center gap-2"><Loader2 size={12} className="animate-spin" /> Lädt…</div>
      ) : projects.length === 0 ? (
        <div className="bg-bg-secondary rounded-lg p-8 border border-border text-center">
          <FolderOpen size={32} className="mx-auto text-text-dim mb-3" />
          <p className="text-text-muted text-sm">Noch keine Projekte</p>
          <p className="text-text-dim text-xs mt-2">
            Lege ein neues Projekt an oder klicke auf Suchen, um vorhandene Ordner zu übernehmen aus <span className="font-mono text-accent">{projectsRoot || '...'}</span>
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {projects.map(p => (
            <div key={p.name} className="px-4 py-3 bg-bg-secondary rounded-lg border border-border hover:border-text-dim transition-colors">
              <div className="flex items-center justify-between gap-4">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{p.name}</span>
                    <span className={`text-xs capitalize ${frameworkBadge[p.framework] || 'text-text-dim'}`}>{p.app ? 'App' : p.framework === 'unknown' ? 'unbekannt' : p.framework === 'php' ? 'PHP' : p.framework}</span>
                    {p.local_only && (
                      <span className="inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded bg-status-yellow/10 text-status-yellow" title="Nur von diesem Rechner erreichbar">
                        <ShieldCheck size={10} /> nur lokal
                      </span>
                    )}
                  </div>
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 mt-1">
                    <UrlLink url={localURL(p)} icon={<Home size={11} />} title="Lokale Adresse (nur dieser Rechner)" />
                    {(p.aliases || []).map(a => (
                      <UrlLink key={a} url={`https://${a}`} icon={<Globe size={11} />} title="Öffentliche Adresse über Cloudflare Tunnel" />
                    ))}
                  </div>
                  <div className="text-[11px] text-text-dim font-mono truncate mt-1" title={p.document_root}>
                    {p.framework === 'proxy' ? `→ ${p.proxy_target}` : p.document_root}
                  </div>
                </div>
                <div className="flex items-center gap-1 flex-shrink-0">
                  {p.framework !== 'proxy' && phpVersions.length > 0 && (
                    <select
                      value={p.php_version || ''}
                      onChange={e => changePHP(p, e.target.value)}
                      className="px-2 py-1 mr-1 bg-bg-primary border border-border rounded text-xs font-mono text-text-muted focus:outline-none focus:border-accent/50 cursor-pointer"
                      title="PHP-Version für dieses Projekt"
                    >
                      {phpVersions.map(v => <option key={v.version} value={v.version}>PHP {v.version}</option>)}
                    </select>
                  )}
                  <IconButton
                    onClick={() => toggleSSL(p)}
                    title={p.ssl_enabled ? 'Lokales HTTPS an (mkcert) - klicken zum Ausschalten' : 'Lokales HTTPS aus - klicken zum Einschalten'}
                    className={p.ssl_enabled ? 'text-status-green' : ''}
                  >
                    {p.ssl_enabled ? <Lock size={13} /> : <Unlock size={13} />}
                  </IconButton>
                  {(p.framework === 'proxy' || p.framework === 'unknown') && (
                    <IconButton onClick={() => setTool({ kind: 'app', project: p })}
                      title={p.app ? `App: ${p.app.command} (Port ${p.app.port})` : 'Als App betreiben (Node, Python, …) - Startbefehl und Dienst'}
                      className={p.app ? 'text-accent' : ''}>
                      <Play size={13} />
                    </IconButton>
                  )}
                  <IconButton onClick={() => setTool({ kind: 'db', project: p })}
                    title={p.database ? `Datenbank: ${p.database.name} (${p.database.type})` : 'Datenbank - anlegen oder zuweisen'}
                    className={p.database ? 'text-status-green' : ''}>
                    <Database size={13} />
                  </IconButton>
                  <IconButton onClick={() => setTool({ kind: 'mail', project: p })}
                    title={p.mail ? `Mail über ${p.mail.host}` : 'Mail - derzeit Mailpit (Test)'}
                    className={p.mail ? 'text-status-green' : ''}>
                    <Mail size={13} />
                  </IconButton>
                  {!!p.path && (
                    <IconButton onClick={() => setTool({ kind: 'git', project: p })} title="Git - Pull, Commit & Push"><GitBranch size={13} /></IconButton>
                  )}
                  <IconButton onClick={() => openFolder(p)} title="Ordner im Explorer öffnen"><FolderOpen size={13} /></IconButton>
                  <IconButton onClick={() => setEditing(p)} title="Projekt bearbeiten"><Pencil size={13} /></IconButton>
                  <IconButton onClick={() => confirmRemove(p)} title="Projekt entfernen" className="hover:!text-status-red"><Trash2 size={13} /></IconButton>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function IconButton({ children, onClick, title, className = '' }: { children: React.ReactNode; onClick: () => void; title: string; className?: string }) {
  return (
    <button onClick={onClick} title={title} aria-label={title}
      className={`p-1.5 rounded text-text-dim hover:text-text-primary hover:bg-bg-primary transition-colors ${className}`}>
      {children}
    </button>
  )
}

function UrlLink({ url, icon, title }: { url: string; icon: React.ReactNode; title: string }) {
  return (
    <span className="inline-flex items-center gap-1 group">
      <button onClick={() => openURL(url)} title={`${title} - im Browser öffnen`}
        className="inline-flex items-center gap-1 text-xs font-mono text-accent hover:underline">
        {icon}{url.replace(/^https?:\/\//, '')}
      </button>
      <button
        onClick={() => navigator.clipboard.writeText(url).then(() => toast.info('URL kopiert', url)).catch(() => {})}
        className="opacity-0 group-hover:opacity-100 text-text-dim hover:text-text-primary" title="URL kopieren">
        <Copy size={10} />
      </button>
    </span>
  )
}

function settingsOf(p: Project, patch: Partial<Record<string, any>> = {}) {
  return {
    domain: p.domain,
    aliases: p.aliases || [],
    path: p.path,
    document_root: p.document_root,
    php_version: p.php_version,
    proxy_target: p.proxy_target || '',
    ssl_enabled: p.ssl_enabled,
    local_only: !!p.local_only,
    ...patch,
  }
}

// --- Remove -------------------------------------------------------------------

function RemoveProjectDialog({ project, projectsRoot, onClose, onRemoved }: {
  project: Project
  projectsRoot: string
  onClose: () => void
  onRemoved: () => void
}) {
  const norm = (s: string) => s.replace(/[\\/]+$/, '').toLowerCase()
  const folderDeletable = !!project.path && norm(project.path.replace(/[\\/][^\\/]+$/, '')) === norm(projectsRoot)
  // Removing a project usually means "get rid of it": both are on by default.
  const [deleteFolder, setDeleteFolder] = useState(folderDeletable)
  const [dropDatabase, setDropDatabase] = useState(!!project.database)

  const [remove, removing] = useAction(async () => {
    const backup = await call<string>('RemoveProject', project.name, deleteFolder, dropDatabase)
    onRemoved()
    return backup
  }, {
    success: backup => [
      `Projekt ${project.name} entfernt`,
      deleteFolder ? 'Ordner gelöscht' : 'Ordner bleibt erhalten',
      backup ? `Datenbank gelöscht (Backup: ${backup.split(/[\\/]/).pop()})` : '',
    ].filter(Boolean).join(' · '),
    error: 'Projekt konnte nicht entfernt werden',
  })

  return (
    <Modal title={`Projekt ${project.name} entfernen`} onClose={onClose} width="max-w-lg">
      <div className="space-y-4">
        <p className="text-sm text-text-muted">
          Die Webserver-Konfiguration und der hosts-Eintrag werden entfernt; <span className="font-mono text-text-primary">{project.domain}</span> ist danach nicht mehr erreichbar.
        </p>
        <div className="space-y-3">
          <label className={`flex items-start gap-2 text-sm ${folderDeletable ? 'text-text-muted cursor-pointer' : 'text-text-dim'}`}>
            <input type="checkbox" className="accent-accent mt-0.5" checked={deleteFolder} disabled={!folderDeletable}
              onChange={e => setDeleteFolder(e.target.checked)} />
            <span>
              Ordner mit allen Dateien löschen
              <span className="block text-[11px] font-mono text-text-dim break-all">{project.path || '(kein Ordner)'}</span>
              {!folderDeletable && project.path && <span className="block text-[11px] text-text-dim">Liegt nicht im Projektordner - wird zur Sicherheit nicht gelöscht.</span>}
            </span>
          </label>
          {project.database && (
            <label className="flex items-start gap-2 text-sm text-text-muted cursor-pointer">
              <input type="checkbox" className="accent-accent mt-0.5" checked={dropDatabase} onChange={e => setDropDatabase(e.target.checked)} />
              <span>
                Datenbank <span className="font-mono text-text-primary">{project.database.name}</span> löschen
                <span className="block text-[11px] text-text-dim">Vorher wird automatisch ein Backup gespeichert.</span>
              </span>
            </label>
          )}
        </div>
        {!deleteFolder && folderDeletable && (
          <p className="text-[11px] text-text-dim">
            Der Ordner bleibt liegen und wird beim Start nicht wieder als Projekt aufgenommen. „Suchen“ holt ihn bei Bedarf zurück.
          </p>
        )}
        {deleteFolder && (
          <p className="text-xs text-status-red">Das Löschen des Ordners kann nicht rückgängig gemacht werden.</p>
        )}
        <div className="flex justify-end gap-2 pt-2 border-t border-border">
          <Button onClick={onClose}>Abbrechen</Button>
          <Button variant="danger" busy={removing} onClick={remove}>
            {deleteFolder || dropDatabase ? 'Endgültig entfernen' : 'Projekt entfernen'}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

// --- Edit ---------------------------------------------------------------------

function EditProject({ project, phpVersions, onClose, onSaved }: {
  project: Project
  phpVersions: PHPVersion[]
  onClose: () => void
  onSaved: () => void
}) {
  const [form, setForm] = useState(() => ({
    ...settingsOf(project),
    aliasesText: (project.aliases || []).join('\n'),
  }))
  const isProxy = project.framework === 'proxy'
  const set = (patch: Partial<typeof form>) => setForm(f => ({ ...f, ...patch }))

  const [save, saving] = useAction(async () => {
    const aliases = form.aliasesText.split(/[\s,]+/).map(s => s.trim()).filter(Boolean)
    const { aliasesText, ...rest } = form
    await call('UpdateProjectSettings', project.name, { ...rest, aliases })
    const added = aliases.filter(a => !(project.aliases || []).includes(a))
    return added
  }, {
    success: added => added.length
      ? `Gespeichert - ${added.join(', ')} hinzugefügt. Zum Veröffentlichen auf der Seite Tunnel „Sync from projects“ klicken.`
      : 'Projekt gespeichert',
    error: 'Projekt konnte nicht gespeichert werden',
  })

  async function browse(field: 'path' | 'document_root') {
    try {
      const dir = await call<string>('PickProjectDirectory')
      if (dir) set({ [field]: dir } as any)
    } catch { /* cancelled */ }
  }

  return (
    <Modal title={`${project.name} bearbeiten`} onClose={onClose}>
      <div className="space-y-4">
        <Field label="Lokale Domain" hint="Nur auf diesem Rechner erreichbar (hosts-Datei), z. B. name.test oder name.local">
          <input className={inputCls} value={form.domain} onChange={e => set({ domain: e.target.value })} />
        </Field>

        <Field
          label="Öffentliche Domains"
          hint={<>Eine pro Zeile, z. B. <span className="font-mono">blog.example.com</span>. Die Seite antwortet auf diese Namen; veröffentlicht werden sie auf der Seite Tunnel (Sync from projects, dann Create DNS).</>}
        >
          <textarea
            className={`${inputCls} h-20 resize-y`}
            value={form.aliasesText}
            onChange={e => set({ aliasesText: e.target.value })}
            placeholder="www.example.com"
          />
        </Field>

        {isProxy ? (
          <Field label="Proxy-Ziel" hint="Die App, an die diese Seite weiterleitet, z. B. http://127.0.0.1:3000">
            <input className={inputCls} value={form.proxy_target} onChange={e => set({ proxy_target: e.target.value })} />
          </Field>
        ) : (
          <>
            <Field label="Projektordner">
              <div className="flex gap-2">
                <input className={inputCls} value={form.path} onChange={e => set({ path: e.target.value })} />
                <Button onClick={() => browse('path')}>Durchsuchen…</Button>
              </div>
            </Field>
            <Field label="Document Root" hint="Der Ordner, den der Webserver ausliefert, z. B. public/ bei Laravel.">
              <div className="flex gap-2">
                <input className={inputCls} value={form.document_root} onChange={e => set({ document_root: e.target.value })} />
                <Button onClick={() => browse('document_root')}>Durchsuchen…</Button>
              </div>
            </Field>
            {phpVersions.length > 0 && (
              <Field label="PHP-Version">
                <select className={inputCls} value={form.php_version} onChange={e => set({ php_version: e.target.value })}>
                  {phpVersions.map(v => <option key={v.version} value={v.version}>PHP {v.version}</option>)}
                </select>
              </Field>
            )}
          </>
        )}

        <div className="space-y-2 pt-1">
          <label className="flex items-center gap-2 text-sm text-text-muted cursor-pointer">
            <input type="checkbox" className="accent-accent" checked={form.ssl_enabled} onChange={e => set({ ssl_enabled: e.target.checked })} />
            Lokales HTTPS mit mkcert-Zertifikat
            <span className="text-[11px] text-text-dim">(für den Tunnel nicht nötig - Cloudflare liefert HTTPS)</span>
          </label>
          <label className="flex items-center gap-2 text-sm text-text-muted cursor-pointer">
            <input type="checkbox" className="accent-accent" checked={form.local_only} onChange={e => set({ local_only: e.target.checked })} />
            Nur von diesem Rechner erreichbar
          </label>
        </div>

        <div className="flex justify-end gap-2 pt-2 border-t border-border">
          <Button onClick={onClose}>Abbrechen</Button>
          <Button variant="primary" busy={saving} onClick={async () => { if (await save() !== undefined) onSaved() }}>
            Speichern
          </Button>
        </div>
      </div>
    </Modal>
  )
}

// --- Create -------------------------------------------------------------------

const frameworks = [
  { value: 'plain', label: 'PHP (leer)', placeholder: '', description: 'Leerer Ordner mit Startseite' },
  { value: 'laravel', label: 'Laravel', placeholder: '11.*', description: 'composer create-project laravel/laravel' },
  { value: 'symfony', label: 'Symfony', placeholder: '7.*', description: 'composer create-project symfony/skeleton' },
  { value: 'wordpress', label: 'WordPress', placeholder: '', description: 'aktuelle deutsche Version, inklusive Datenbank' },
  { value: 'clone', label: 'Aus Git klonen', placeholder: '', description: 'https-Adresse eines GitHub- / GitLab- / Gitea-Repos' },
  { value: 'app', label: 'Node- / Python-App', placeholder: '', description: 'Hangar betreibt sie als Windows-Dienst' },
  { value: 'proxy', label: 'Proxy', placeholder: '', description: 'Weiterleitung an eine App, die anderswo läuft' },
]

function CreateProject({ projectsRoot, domainSuffix, onClose, onCreated }: {
  projectsRoot: string
  domainSuffix: string
  onClose: () => void
  onCreated: (name: string, detail?: string, openApp?: boolean) => void
}) {
  const [p, setP] = useState({ name: '', framework: 'plain', version: '', proxyTarget: '', path: '', laravelDocRoot: 'public', gitUrl: '', domain: '' })
  const [nameTouched, setNameTouched] = useState(false)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const set = (patch: Partial<typeof p>) => setP(prev => ({ ...prev, ...patch }))
  const name = p.name.trim()
  const nameValid = /^[a-z0-9][a-z0-9-]{0,62}$/.test(name)
  const domain = p.domain.trim().toLowerCase()

  async function create() {
    if (!name) return
    setError('')
    setCreating(true)
    try {
      let detail: string | undefined
      let openApp = false
      if (p.framework === 'app') {
        const port = await call<number>('SuggestAppPort')
        await call('CreateProjectWithOptions', {
          name,
          path: p.path.trim() || `${projectsRoot}\\${name}`,
          domain,
          framework: 'proxy',
          proxy_target: `http://127.0.0.1:${port}`,
        })
        detail = 'Jetzt den Startbefehl eintragen und den Dienst installieren.'
        openApp = true
      } else if (p.framework === 'clone') {
        await call('CloneProject', { url: p.gitUrl.trim(), name, path: p.path.trim() })
        detail = 'Geklont. Falls nötig, im Terminal composer install / npm install ausführen.'
      } else if (p.framework === 'proxy' || p.framework === 'plain') {
        await call('CreateProjectWithOptions', {
          name,
          path: p.path.trim(),
          domain,
          framework: p.framework === 'proxy' ? 'proxy' : 'php',
          proxy_target: p.proxyTarget.trim(),
        })
      } else {
        const result = await call<Record<string, string>>('CreateProjectWithFramework', p.framework, name, p.version)
        if (result?.error) throw new Error(result.error)
        if (p.framework === 'wordpress') detail = result?.output
        if (p.framework === 'laravel' && p.laravelDocRoot === 'root') {
          const all = await call<Project[]>('GetProjects')
          const me = all.find(x => x.name === name)
          if (me) await call('UpdateProjectSettings', name, settingsOf(me, { document_root: me.path }))
        }
      }
      if (domain && (p.framework === 'clone' || !['app', 'proxy', 'plain'].includes(p.framework))) {
        const all = await call<Project[]>('GetProjects')
        const me = all.find(x => x.name === name)
        if (me && me.domain !== domain) await call('UpdateProjectSettings', name, settingsOf(me, { domain }))
      }
      onCreated(name, detail, openApp)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="mb-6 bg-bg-secondary rounded-lg p-4 border border-accent/20">
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-medium">Projekt anlegen</h3>
        <button onClick={onClose} className="text-text-dim hover:text-text-primary"><X size={14} /></button>
      </div>
      <div className="space-y-3">
        <Field label="Projektname" hint={name && !nameValid
          ? <span className="text-status-red">Nur Kleinbuchstaben, Ziffern und -. Eine Domain wie blog.local gehört ins Feld darunter.</span>
          : 'Wird für den Ordner und den Datenbanknamen verwendet.'}>
          <input className={inputCls} value={p.name} onChange={e => { setNameTouched(true); set({ name: e.target.value.toLowerCase() }) }} placeholder="my-site" disabled={creating} autoFocus />
        </Field>
        <Field label="Lokale Domain (optional)" hint={<>Auf diesem Rechner über die hosts-Datei erreichbar. Standard: <span className="font-mono">{(nameValid ? name : 'my-site') + domainSuffix}</span></>}>
          <input className={inputCls} value={p.domain} onChange={e => set({ domain: e.target.value })} placeholder={`${nameValid ? name : 'my-site'}${domainSuffix}`} disabled={creating} />
        </Field>
        {p.framework === 'clone' && (
          <Field label="Repository-URL" hint="Nur https. Bei privaten Repos öffnet Git ein Login-Fenster (Git Credential Manager) und merkt sich die Anmeldung.">
            <input className={inputCls} value={p.gitUrl} disabled={creating} placeholder="https://github.com/user/repo.git"
              onChange={e => {
                const gitUrl = e.target.value
                const base = gitUrl.trim().replace(/\/+$/, '').split('/').pop()?.replace(/\.git$/i, '') || ''
                set(nameTouched ? { gitUrl } : { gitUrl, name: base.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') })
              }} />
          </Field>
        )}
        {p.framework !== 'proxy' && p.framework !== 'wordpress' && (
          <Field label="Ordner" hint={p.path ? undefined : <>Leer lassen für <span className="font-mono">{projectsRoot}\{name || 'my-site'}</span></>}>
            <div className="flex gap-2">
              <input className={inputCls} value={p.path} onChange={e => set({ path: e.target.value })} disabled={creating} placeholder={`${projectsRoot}\\${name || 'my-site'}`} />
              <Button disabled={creating} onClick={async () => {
                try { const dir = await call<string>('PickProjectDirectory'); if (dir) set({ path: dir }) } catch { /* cancelled */ }
              }}>Durchsuchen…</Button>
            </div>
          </Field>
        )}
        <Field label="Typ">
          <select className={inputCls} value={p.framework} onChange={e => set({ framework: e.target.value, version: '' })} disabled={creating}>
            {frameworks.map(f => <option key={f.value} value={f.value}>{f.label} - {f.description}</option>)}
          </select>
        </Field>
        {(p.framework === 'laravel' || p.framework === 'symfony') && (
          <Field label="Version (optional)">
            <input className={inputCls} value={p.version} onChange={e => set({ version: e.target.value })}
              placeholder={frameworks.find(f => f.value === p.framework)?.placeholder} disabled={creating} />
          </Field>
        )}
        {p.framework === 'laravel' && (
          <Field label="Document Root">
            <select className={inputCls} value={p.laravelDocRoot} onChange={e => set({ laravelDocRoot: e.target.value })} disabled={creating}>
              <option value="public">/public (Laravel-Standard)</option>
              <option value="root">Projektordner</option>
            </select>
          </Field>
        )}
        {p.framework === 'proxy' && (
          <Field label="Proxy-Ziel" hint="Die Adresse der App, z. B. http://127.0.0.1:3000. Jede Anfrage an diese Seite wird dorthin weitergeleitet.">
            <input className={inputCls} value={p.proxyTarget} onChange={e => set({ proxyTarget: e.target.value })} placeholder="http://127.0.0.1:3000" disabled={creating} />
          </Field>
        )}
        {error && <div className="text-xs text-status-red whitespace-pre-wrap">{error}</div>}
        <Button variant="primary" busy={creating} disabled={!nameValid || (p.framework === 'clone' && !/^https:\/\/[^/]+\/.+/.test(p.gitUrl.trim()))} onClick={create}>
          {creating ? (p.framework === 'plain' || p.framework === 'proxy' ? 'Wird angelegt…' : 'Wird installiert - das kann ein paar Minuten dauern…') : 'Anlegen'}
        </Button>
      </div>
    </div>
  )
}
