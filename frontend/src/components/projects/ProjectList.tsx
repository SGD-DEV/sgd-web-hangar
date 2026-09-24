import { useState, useEffect, useCallback } from 'react'
import { FolderOpen, Plus, Trash2, Search, X, Loader2, Lock, Unlock, Pencil, Globe, Home, ShieldCheck, Copy } from 'lucide-react'
import { call, toast, useAction, errorMessage } from '../../lib/api'
import { Button, Modal, Field, inputCls } from '../ui/Controls'

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
    toast.error('Could not open browser', errorMessage(e))
  }
}

export default function ProjectList() {
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [editing, setEditing] = useState<Project | null>(null)
  const [projectsRoot, setProjectsRoot] = useState('')
  const [phpVersions, setPhpVersions] = useState<PHPVersion[]>([])
  const [webServer, setWebServer] = useState('apache')

  const loadProjects = useCallback(async () => {
    try {
      const p = await call<Project[]>('GetProjects')
      setProjects((p || []).sort((a, b) => a.name.localeCompare(b.name)))
    } catch (e) {
      toast.error('Could not load projects', errorMessage(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadProjects()
    call<string>('GetProjectsRoot').then(r => r && setProjectsRoot(r)).catch(() => {})
    call<PHPVersion[]>('GetInstalledPHPVersions').then(v => setPhpVersions(v || [])).catch(() => {})
    call<string>('GetActiveWebServer').then(setWebServer).catch(() => {})
  }, [loadProjects])

  const [scan, scanning] = useAction(async () => {
    const found = await call<Project[]>('ScanProjects')
    await loadProjects()
    return found?.length || 0
  }, { success: n => n ? `${n} new project${n === 1 ? '' : 's'} found` : 'No new projects found', error: 'Scan failed' })

  const [toggleSSL] = useAction(async (p: Project) => {
    await call('UpdateProjectSettings', p.name, settingsOf(p, { ssl_enabled: !p.ssl_enabled }))
    await loadProjects()
    return !p.ssl_enabled
  }, { success: on => on ? 'HTTPS enabled' : 'HTTPS disabled', error: 'Could not change HTTPS' })

  const [changePHP] = useAction(async (p: Project, version: string) => {
    await call('UpdateProjectSettings', p.name, settingsOf(p, { php_version: version }))
    await loadProjects()
    return version
  }, { success: v => `PHP ${v} is now used`, error: 'Could not switch PHP' })

  const [remove] = useAction(async (p: Project) => {
    await call('DeleteProject', p.name)
    await loadProjects()
  }, { success: 'Project removed (files were kept)', error: 'Could not remove project' })

  const [openFolder] = useAction((p: Project) => call('OpenInExplorer', p.path), { error: 'Could not open folder' })

  function confirmRemove(p: Project) {
    if (window.confirm(`Remove project "${p.name}"?\n\nThe web server config and hosts entry are removed. The folder ${p.path} is NOT deleted.`)) {
      remove(p)
    }
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
          <h2 className="text-lg font-medium">Projects</h2>
          <p className="text-xs text-text-muted mt-1">
            Sites served by <span className="text-text-primary capitalize">{webServer}</span> from{' '}
            <span className="font-mono text-text-primary">{projectsRoot || '...'}</span>
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button onClick={scan} busy={scanning} icon={<Search size={12} />} title="Register folders in the projects root that aren't projects yet">
            Scan
          </Button>
          <Button variant="primary" onClick={() => setShowCreate(true)} icon={<Plus size={12} />}>
            New Project
          </Button>
        </div>
      </div>

      {showCreate && (
        <CreateProject
          projectsRoot={projectsRoot}
          onClose={() => setShowCreate(false)}
          onCreated={async (name) => {
            setShowCreate(false)
            await loadProjects()
            toast.success(`Project ${name} created`, 'Use the pencil icon to add a public domain for the Cloudflare Tunnel.')
          }}
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
        <div className="text-text-dim text-sm flex items-center gap-2"><Loader2 size={12} className="animate-spin" /> Loading...</div>
      ) : projects.length === 0 ? (
        <div className="bg-bg-secondary rounded-lg p-8 border border-border text-center">
          <FolderOpen size={32} className="mx-auto text-text-dim mb-3" />
          <p className="text-text-muted text-sm">No projects yet</p>
          <p className="text-text-dim text-xs mt-2">
            Create a new project or click Scan to pick up folders in <span className="font-mono text-accent">{projectsRoot || '...'}</span>
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
                    <span className={`text-xs capitalize ${frameworkBadge[p.framework] || 'text-text-dim'}`}>{p.framework}</span>
                    {p.local_only && (
                      <span className="inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded bg-status-yellow/10 text-status-yellow" title="Only reachable from this machine">
                        <ShieldCheck size={10} /> local only
                      </span>
                    )}
                  </div>
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 mt-1">
                    <UrlLink url={localURL(p)} icon={<Home size={11} />} title="Local address (this machine only)" />
                    {(p.aliases || []).map(a => (
                      <UrlLink key={a} url={`https://${a}`} icon={<Globe size={11} />} title="Public address via Cloudflare Tunnel" />
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
                      title="PHP version for this project"
                    >
                      {phpVersions.map(v => <option key={v.version} value={v.version}>PHP {v.version}</option>)}
                    </select>
                  )}
                  <IconButton
                    onClick={() => toggleSSL(p)}
                    title={p.ssl_enabled ? 'Local HTTPS on (mkcert) - click to turn off' : 'Local HTTPS off - click to turn on'}
                    className={p.ssl_enabled ? 'text-status-green' : ''}
                  >
                    {p.ssl_enabled ? <Lock size={13} /> : <Unlock size={13} />}
                  </IconButton>
                  <IconButton onClick={() => openFolder(p)} title="Open folder in Explorer"><FolderOpen size={13} /></IconButton>
                  <IconButton onClick={() => setEditing(p)} title="Edit project"><Pencil size={13} /></IconButton>
                  <IconButton onClick={() => confirmRemove(p)} title="Remove project" className="hover:!text-status-red"><Trash2 size={13} /></IconButton>
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
      <button onClick={() => openURL(url)} title={`${title} - open in browser`}
        className="inline-flex items-center gap-1 text-xs font-mono text-accent hover:underline">
        {icon}{url.replace(/^https?:\/\//, '')}
      </button>
      <button
        onClick={() => navigator.clipboard.writeText(url).then(() => toast.info('URL copied', url)).catch(() => {})}
        className="opacity-0 group-hover:opacity-100 text-text-dim hover:text-text-primary" title="Copy URL">
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
      ? `Saved - ${added.join(', ')} added. Open the Tunnel page and click "Sync from projects" to publish it.`
      : 'Project saved',
    error: 'Could not save project',
  })

  async function browse(field: 'path' | 'document_root') {
    try {
      const dir = await call<string>('PickProjectDirectory')
      if (dir) set({ [field]: dir } as any)
    } catch { /* cancelled */ }
  }

  return (
    <Modal title={`Edit ${project.name}`} onClose={onClose}>
      <div className="space-y-4">
        <Field label="Local domain" hint="Reachable on this machine only (hosts file). Must end in a name the hosts file can resolve, e.g. .test">
          <input className={inputCls} value={form.domain} onChange={e => set({ domain: e.target.value })} />
        </Field>

        <Field
          label="Public domains"
          hint={<>One per line, e.g. <span className="font-mono">blog.example.com</span>. The site answers to these names; publish them on the Tunnel page (Sync from projects, then Create DNS).</>}
        >
          <textarea
            className={`${inputCls} h-20 resize-y`}
            value={form.aliasesText}
            onChange={e => set({ aliasesText: e.target.value })}
            placeholder="www.example.com"
          />
        </Field>

        {isProxy ? (
          <Field label="Proxy target" hint="The app this site forwards to, e.g. http://127.0.0.1:3000">
            <input className={inputCls} value={form.proxy_target} onChange={e => set({ proxy_target: e.target.value })} />
          </Field>
        ) : (
          <>
            <Field label="Project folder">
              <div className="flex gap-2">
                <input className={inputCls} value={form.path} onChange={e => set({ path: e.target.value })} />
                <Button onClick={() => browse('path')}>Browse...</Button>
              </div>
            </Field>
            <Field label="Document root" hint="The folder the web server serves, e.g. the public/ folder of a Laravel app.">
              <div className="flex gap-2">
                <input className={inputCls} value={form.document_root} onChange={e => set({ document_root: e.target.value })} />
                <Button onClick={() => browse('document_root')}>Browse...</Button>
              </div>
            </Field>
            {phpVersions.length > 0 && (
              <Field label="PHP version">
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
            Local HTTPS with a mkcert certificate
            <span className="text-[11px] text-text-dim">(not needed for the tunnel - Cloudflare serves HTTPS)</span>
          </label>
          <label className="flex items-center gap-2 text-sm text-text-muted cursor-pointer">
            <input type="checkbox" className="accent-accent" checked={form.local_only} onChange={e => set({ local_only: e.target.checked })} />
            Only reachable from this machine
          </label>
        </div>

        <div className="flex justify-end gap-2 pt-2 border-t border-border">
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" busy={saving} onClick={async () => { if (await save() !== undefined) onSaved() }}>
            Save
          </Button>
        </div>
      </div>
    </Modal>
  )
}

// --- Create -------------------------------------------------------------------

const frameworks = [
  { value: 'plain', label: 'Plain PHP', placeholder: '', description: 'Empty folder, no scaffolding' },
  { value: 'laravel', label: 'Laravel', placeholder: '11.*', description: 'composer create-project laravel/laravel' },
  { value: 'symfony', label: 'Symfony', placeholder: '7.*', description: 'composer create-project symfony/skeleton' },
  { value: 'wordpress', label: 'WordPress', placeholder: '', description: 'wp-cli core download' },
  { value: 'proxy', label: 'Proxy', placeholder: '', description: 'Forward to an app already running (Node, Python, Go, ...)' },
]

function CreateProject({ projectsRoot, onClose, onCreated }: {
  projectsRoot: string
  onClose: () => void
  onCreated: (name: string) => void
}) {
  const [p, setP] = useState({ name: '', framework: 'plain', version: '', proxyTarget: '', path: '', laravelDocRoot: 'public' })
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const set = (patch: Partial<typeof p>) => setP(prev => ({ ...prev, ...patch }))
  const name = p.name.trim()

  async function create() {
    if (!name) return
    setError('')
    setCreating(true)
    try {
      if (p.framework === 'proxy' || p.framework === 'plain') {
        await call('CreateProjectWithOptions', {
          name,
          path: p.path.trim(),
          domain: '',
          framework: p.framework === 'proxy' ? 'proxy' : 'php',
          proxy_target: p.proxyTarget.trim(),
        })
      } else {
        const result = await call<Record<string, string>>('CreateProjectWithFramework', p.framework, name, p.version)
        if (result?.error) throw new Error(result.error)
        if (p.framework === 'laravel' && p.laravelDocRoot === 'root') {
          const all = await call<Project[]>('GetProjects')
          const me = all.find(x => x.name === name)
          if (me) await call('UpdateProjectSettings', name, settingsOf(me, { document_root: me.path }))
        }
      }
      onCreated(name)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="mb-6 bg-bg-secondary rounded-lg p-4 border border-accent/20">
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-medium">Create Project</h3>
        <button onClick={onClose} className="text-text-dim hover:text-text-primary"><X size={14} /></button>
      </div>
      <div className="space-y-3">
        <Field label="Project name" hint={name ? <>Local address: <span className="font-mono">http://{name.toLowerCase()}.test</span></> : undefined}>
          <input className={inputCls} value={p.name} onChange={e => set({ name: e.target.value })} placeholder="my-site" disabled={creating} autoFocus />
        </Field>
        {p.framework !== 'proxy' && (
          <Field label="Folder" hint={p.path ? undefined : <>Leave empty to use <span className="font-mono">{projectsRoot}\{name || 'my-site'}</span></>}>
            <div className="flex gap-2">
              <input className={inputCls} value={p.path} onChange={e => set({ path: e.target.value })} disabled={creating} placeholder={`${projectsRoot}\\${name || 'my-site'}`} />
              <Button disabled={creating} onClick={async () => {
                try { const dir = await call<string>('PickProjectDirectory'); if (dir) set({ path: dir }) } catch { /* cancelled */ }
              }}>Browse...</Button>
            </div>
          </Field>
        )}
        <Field label="Type">
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
          <Field label="Document root">
            <select className={inputCls} value={p.laravelDocRoot} onChange={e => set({ laravelDocRoot: e.target.value })} disabled={creating}>
              <option value="public">/public (Laravel default)</option>
              <option value="root">Project root</option>
            </select>
          </Field>
        )}
        {p.framework === 'proxy' && (
          <Field label="Proxy target" hint="The address of the app, e.g. http://127.0.0.1:3000. Every request to this site is forwarded there.">
            <input className={inputCls} value={p.proxyTarget} onChange={e => set({ proxyTarget: e.target.value })} placeholder="http://127.0.0.1:3000" disabled={creating} />
          </Field>
        )}
        {error && <div className="text-xs text-status-red whitespace-pre-wrap">{error}</div>}
        <Button variant="primary" busy={creating} disabled={!name} onClick={create}>
          {creating ? (p.framework === 'plain' || p.framework === 'proxy' ? 'Creating...' : 'Installing - this can take a few minutes...') : 'Create'}
        </Button>
      </div>
    </div>
  )
}
