import { useState, useEffect } from 'react'
import { FolderOpen, Plus, ExternalLink, Trash2, Search, X, Loader2, Lock, Unlock } from 'lucide-react'

interface Project {
  name: string
  path: string
  domain: string
  php_version: string
  web_server: string
  ssl_enabled: boolean
  framework: string
  document_root: string
}

interface PHPVersion {
  version: string
  is_active: boolean
}

export default function ProjectList() {
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [projectsRoot, setProjectsRoot] = useState('')
  const [newProject, setNewProject] = useState({
    name: '',
    domain: '',
    framework: 'plain',  // sensible default: don't scaffold anything
    version: '',
    proxyTarget: '',
    path: '',
    webServer: 'apache',
    sslEnabled: false,
    laravelDocRoot: 'public', // 'public' or 'root' (only used for Laravel)
  })
  const [createError, setCreateError] = useState('')
  const [creating, setCreating] = useState(false)
  const [phpVersions, setPhpVersions] = useState<PHPVersion[]>([])
  // Which web servers are installed (so we only show options the user can use).
  const [webServersAvail, setWebServersAvail] = useState<{ apache: boolean; nginx: boolean }>({ apache: false, nginx: false })

  useEffect(() => {
    loadProjects()
    loadProjectsRoot()
    loadPHPVersions()
    loadWebServers()
  }, [])

  function resetNewProject() {
    setNewProject({
      name: '', domain: '', framework: 'plain', version: '', proxyTarget: '',
      path: '', webServer: 'apache', sslEnabled: false, laravelDocRoot: 'public',
    })
  }

  async function loadWebServers() {
    try {
      // @ts-ignore Wails bindings
      const all = await window.go?.app?.App?.GetAllServiceStatuses?.()
      // A service is "available" if it's installed (port present in status,
      // even when stopped). Stopped is fine - the user can start it after.
      const apacheOk = !!(all && all.apache && (all.apache.status !== undefined))
      const nginxOk = !!(all && all.nginx && (all.nginx.status !== undefined))
      setWebServersAvail({ apache: apacheOk, nginx: nginxOk })
      // Default to whichever is currently RUNNING; else keep the existing
      // selection so the user's choice isn't reset when they switch screens.
      setNewProject(prev => {
        if (prev.webServer === 'apache' && !apacheOk && nginxOk) return { ...prev, webServer: 'nginx' }
        if (prev.webServer === 'nginx' && !nginxOk && apacheOk) return { ...prev, webServer: 'apache' }
        return prev
      })
    } catch { /* best-effort */ }
  }

  async function loadProjectsRoot() {
    try {
      const root = await window.go?.app?.App?.GetProjectsRoot?.()
      if (root) setProjectsRoot(root)
    } catch (e) {
      console.error(e)
    }
  }

  async function loadProjects() {
    try {
      if (window.go?.app?.App?.GetProjects) {
        const p = await window.go.app.App.GetProjects()
        setProjects(p || [])
      }
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }

  async function handleCreate() {
    if (!newProject.name) return
    setCreateError('')
    setCreating(true)
    try {
      // Proxy projects don't run any composer/npm scaffolding - they just
      // need a vhost pointing at an external service. Use the
      // CreateProjectWithOptions endpoint instead of the framework one.
      if (newProject.framework === 'proxy') {
        if (!newProject.proxyTarget.trim()) {
          setCreateError('Proxy target URL is required (e.g. http://localhost:8000)')
          setCreating(false)
          return
        }
        // @ts-ignore Wails bindings
        await window.go.app.App.CreateProjectWithOptions({
          name: newProject.name.trim(),
          path: newProject.path.trim(),
          domain: '',
          framework: 'proxy',
          proxy_target: newProject.proxyTarget.trim(),
          web_server: newProject.webServer,
          ssl_enabled: newProject.sslEnabled,
        })
        setShowCreate(false)
        resetNewProject()
        await loadProjects()
        setCreating(false)
        return
      }

      // Plain PHP / proxy / unknown frameworks: don't scaffold via composer
      // / wp-cli / symfony installer - just create a directory and write the
      // vhost. The framework-scaffolding path below only runs for Laravel /
      // Symfony / WordPress where we actually have a setup command.
      if (newProject.framework === 'plain' || newProject.framework === 'php') {
        // @ts-ignore Wails bindings
        await window.go.app.App.CreateProjectWithOptions({
          name: newProject.name.trim(),
          path: newProject.path.trim(),
          domain: '',
          framework: 'php',
          web_server: newProject.webServer,
          ssl_enabled: newProject.sslEnabled,
        })
        setShowCreate(false)
        resetNewProject()
        await loadProjects()
        setCreating(false)
        return
      }

      const result = await window.go.app.App.CreateProjectWithFramework(
        newProject.framework,
        newProject.name.trim(),
        newProject.version
      )
      if (result.error) {
        setCreateError(result.error)
      } else {
        // Now apply per-project overrides (web server, SSL, doc root for
        // Laravel) by patching the freshly-created project. Wails handles
        // the marshaling of the partial Project struct.
        try {
          // @ts-ignore Wails bindings
          const all = await window.go?.app?.App?.GetProjects?.()
          const me = (all || []).find((p: any) => p.name === newProject.name.trim())
          if (me) {
            me.web_server = newProject.webServer
            me.ssl_enabled = newProject.sslEnabled
            // Laravel: respect the user's choice between public/ and the
            // project root as document root. Other frameworks have their
            // own correct default already.
            if (newProject.framework === 'laravel' && newProject.laravelDocRoot === 'root') {
              me.document_root = me.path
            }
            // @ts-ignore Wails bindings
            await window.go?.app?.App?.UpdateProject?.(me.name, me)
          }
        } catch (e) { /* best effort - vhost is already written */ }

        setShowCreate(false)
        resetNewProject()
        await loadProjects()
      }
    } catch (e: any) {
      setCreateError(e?.message || String(e))
    } finally {
      setCreating(false)
    }
  }

  async function handleScan() {
    try {
      await window.go?.app?.App?.ScanProjects?.()
      await loadProjects()
    } catch (e) {
      console.error(e)
    }
  }

  async function handleToggleSSL(p: Project) {
    try {
      const updated = { ...p, ssl_enabled: !p.ssl_enabled }
      // @ts-ignore Wails bindings
      await window.go?.app?.App?.UpdateProject?.(p.name, updated)
      // UpdateProject re-runs EnsureProjectsReady which generates the cert
      // (if turning on) and reloads the running web server, so the new
      // HTTPS vhost is live immediately.
      await loadProjects()
    } catch (e) {
      console.error('toggle ssl:', e)
    }
  }

  async function handleDelete(name: string) {
    try {
      await window.go?.app?.App?.DeleteProject?.(name)
      await loadProjects()
    } catch (e) {
      console.error(e)
    }
  }

  async function loadPHPVersions() {
    try {
      const v = await window.go?.app?.App?.GetInstalledPHPVersions?.()
      setPhpVersions(v || [])
    } catch (e) {
      console.error(e)
    }
  }

  function handleOpenBrowser(domain: string, ssl: boolean) {
    const protocol = ssl ? 'https' : 'http'
    window.open(`${protocol}://${domain}`, '_blank')
  }

  async function handleChangePHP(projectName: string, phpVersion: string) {
    try {
      await window.go?.app?.App?.UpdateProjectPHP?.(projectName, phpVersion)
      await loadProjects()
    } catch (e) {
      console.error(e)
    }
  }

  const frameworkBadge: Record<string, string> = {
    laravel: 'text-status-red',
    wordpress: 'text-blue-400',
    symfony: 'text-status-yellow',
    php: 'text-text-muted',
    unknown: 'text-text-dim',
  }

  const frameworks = [
    // "plain" first because it's the safe default - no scaffolding command
    // runs, just creates a directory and a vhost. Users picking "Laravel"
    // implicitly opt in to composer create-project.
    { value: 'plain', label: 'Plain PHP', placeholder: '', description: 'Empty PHP project (no scaffolding)' },
    { value: 'laravel', label: 'Laravel', placeholder: '11.*', description: 'composer create-project laravel/laravel' },
    { value: 'symfony', label: 'Symfony', placeholder: '7.*', description: 'composer create-project symfony/skeleton' },
    { value: 'wordpress', label: 'WordPress', placeholder: '', description: 'wp-cli core download' },
    { value: 'proxy', label: 'Proxy', placeholder: '', description: 'Forward to a Python/Node/Go app already running' },
  ]

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-lg font-medium">Projects</h2>
          <p className="text-xs text-text-muted mt-1">Manage your local development projects</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={handleScan}
            className="flex items-center gap-2 px-3 py-1.5 bg-bg-secondary border border-border rounded-lg text-xs text-text-muted hover:text-text-primary hover:border-text-dim transition-colors"
          >
            <Search size={12} />
            Scan
          </button>
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-2 px-3 py-1.5 bg-accent text-bg-primary rounded-lg text-xs font-medium hover:bg-accent-hover transition-colors"
          >
            <Plus size={12} />
            New Project
          </button>
        </div>
      </div>

      {/* Create Project Dialog */}
      {showCreate && (
        <div className="mb-6 bg-bg-secondary rounded-lg p-4 border border-accent/20">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-medium">Create Project</h3>
            <button onClick={() => setShowCreate(false)} className="text-text-dim hover:text-text-primary">
              <X size={14} />
            </button>
          </div>
          <div className="space-y-3">
            <div>
              <label className="block text-xs text-text-muted mb-1">Project Name</label>
              <input
                type="text"
                value={newProject.name}
                onChange={e => setNewProject({ ...newProject, name: e.target.value })}
                placeholder="my-app"
                className="w-full px-3 py-1.5 bg-bg-primary border border-border rounded-lg text-sm text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50 font-mono"
                disabled={creating}
              />
            </div>
            <div>
              <label className="block text-xs text-text-muted mb-1">
                Project Path <span className="text-text-dim">(leave empty to use the default)</span>
              </label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={newProject.path}
                  onChange={e => setNewProject({ ...newProject, path: e.target.value })}
                  placeholder={newProject.name ? `${projectsRoot}\\${newProject.name}` : `${projectsRoot}\\my-app`}
                  className="flex-1 px-3 py-1.5 bg-bg-primary border border-border rounded-lg text-sm text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50 font-mono"
                  disabled={creating}
                />
                <button
                  type="button"
                  onClick={async () => {
                    try {
                      // @ts-ignore Wails bindings
                      const dir = await window.go?.app?.App?.PickProjectDirectory?.()
                      if (dir) setNewProject({ ...newProject, path: dir })
                    } catch (e) { /* user cancelled or no dialog */ }
                  }}
                  disabled={creating}
                  className="px-3 py-1.5 bg-bg-primary border border-border rounded-lg text-sm text-text-muted hover:text-text-primary hover:border-text-dim transition-colors"
                  title="Browse folders"
                >
                  Browse...
                </button>
              </div>
              {newProject.path === '' && newProject.name && (
                <p className="text-[11px] text-text-dim mt-1 font-mono">
                  Will use: {projectsRoot}\{newProject.name}
                </p>
              )}
            </div>
            <div>
              <label className="block text-xs text-text-muted mb-1">Framework</label>
              <select
                value={newProject.framework}
                onChange={e => setNewProject({ ...newProject, framework: e.target.value, version: '' })}
                className="w-full px-3 py-1.5 bg-bg-primary border border-border rounded-lg text-sm text-text-primary focus:outline-none focus:border-accent/50"
                disabled={creating}
              >
                {frameworks.map(f => (
                  <option key={f.value} value={f.value}>
                    {f.label} — {f.description}
                  </option>
                ))}
              </select>
            </div>
            {(newProject.framework === 'laravel' || newProject.framework === 'symfony') && (
              <div>
                <label className="block text-xs text-text-muted mb-1">
                  Version (optional, e.g. 11.* for latest Laravel 11)
                </label>
                <input
                  type="text"
                  value={newProject.version}
                  onChange={e => setNewProject({ ...newProject, version: e.target.value })}
                  placeholder={frameworks.find(f => f.value === newProject.framework)?.placeholder}
                  className="w-full px-3 py-1.5 bg-bg-primary border border-border rounded-lg text-sm text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50 font-mono"
                  disabled={creating}
                />
              </div>
            )}
            {/* Web server picker. Only renders options that are installed.
                Apache mod_proxy is loaded by Hangar's httpd.conf so the
                Proxy framework works there too, but Nginx has it natively. */}
            {(webServersAvail.apache || webServersAvail.nginx) && (
              <div>
                <label className="block text-xs text-text-muted mb-1">Web Server</label>
                <div className="flex gap-2">
                  {webServersAvail.apache && (
                    <button
                      type="button"
                      onClick={() => setNewProject({ ...newProject, webServer: 'apache' })}
                      disabled={creating}
                      className={`flex-1 px-3 py-1.5 rounded-lg border text-sm transition-colors ${newProject.webServer === 'apache' ? 'border-accent bg-accent/10 text-accent' : 'border-border bg-bg-primary text-text-muted hover:text-text-primary'}`}
                    >
                      Apache
                    </button>
                  )}
                  {webServersAvail.nginx && (
                    <button
                      type="button"
                      onClick={() => setNewProject({ ...newProject, webServer: 'nginx' })}
                      disabled={creating}
                      className={`flex-1 px-3 py-1.5 rounded-lg border text-sm transition-colors ${newProject.webServer === 'nginx' ? 'border-accent bg-accent/10 text-accent' : 'border-border bg-bg-primary text-text-muted hover:text-text-primary'}`}
                    >
                      Nginx
                    </button>
                  )}
                </div>
                {newProject.framework === 'proxy' && newProject.webServer === 'apache' && (
                  <p className="text-[11px] text-text-dim mt-1">
                    Apache requires mod_proxy. Hangar loads it by default; if you've stripped
                    httpd.conf, switch to Nginx (proxy is built-in).
                  </p>
                )}
              </div>
            )}
            {/* SSL is configured per-project from the project list (lock
                icon on each row). Removed from this form to avoid emitting
                a half-baked HTTPS vhost before mkcert has generated the
                cert - that produced "SSLCertificateFile takes one argument"
                errors on Apache start. */}
            {/* Laravel-only: choose document root. Laravel keeps its index.php
                in /public; if you point Apache at the project root the user
                gets a directory listing instead of the app. Default is /public. */}
            {newProject.framework === 'laravel' && (
              <div>
                <label className="block text-xs text-text-muted mb-1">Document Root</label>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => setNewProject({ ...newProject, laravelDocRoot: 'public' })}
                    disabled={creating}
                    className={`flex-1 px-3 py-1.5 rounded-lg border text-sm transition-colors ${newProject.laravelDocRoot === 'public' ? 'border-accent bg-accent/10 text-accent' : 'border-border bg-bg-primary text-text-muted hover:text-text-primary'}`}
                  >
                    /public (Laravel default)
                  </button>
                  <button
                    type="button"
                    onClick={() => setNewProject({ ...newProject, laravelDocRoot: 'root' })}
                    disabled={creating}
                    className={`flex-1 px-3 py-1.5 rounded-lg border text-sm transition-colors ${newProject.laravelDocRoot === 'root' ? 'border-accent bg-accent/10 text-accent' : 'border-border bg-bg-primary text-text-muted hover:text-text-primary'}`}
                  >
                    Project root
                  </button>
                </div>
              </div>
            )}
            {newProject.framework === 'proxy' && (
              <div>
                <label className="block text-xs text-text-muted mb-1">
                  Proxy target (URL of the app already running on your machine)
                </label>
                <input
                  type="url"
                  value={newProject.proxyTarget}
                  onChange={e => setNewProject({ ...newProject, proxyTarget: e.target.value })}
                  placeholder="http://127.0.0.1:8000"
                  className="w-full px-3 py-1.5 bg-bg-primary border border-border rounded-lg text-sm text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50 font-mono"
                  disabled={creating}
                />
                <p className="text-[11px] text-text-dim mt-1">
                  All requests to {newProject.name || 'project'}.test will be forwarded here.
                  Useful for Python/Node/Go apps - run them however you usually do, then point
                  Hangar at the address.
                </p>
              </div>
            )}
            {createError && (
              <div className="text-xs text-status-red">{createError}</div>
            )}
            <button
              onClick={handleCreate}
              disabled={!newProject.name || creating}
              className="px-4 py-1.5 bg-accent text-bg-primary rounded-lg text-xs font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
            >
              {creating && <Loader2 size={12} className="animate-spin" />}
              {creating ? 'Creating...' : 'Create'}
            </button>
          </div>
        </div>
      )}

      {loading ? (
        <div className="text-text-dim text-sm">Loading...</div>
      ) : projects.length === 0 ? (
        <div className="bg-bg-secondary rounded-lg p-8 border border-border text-center">
          <FolderOpen size={32} className="mx-auto text-text-dim mb-3" />
          <p className="text-text-muted text-sm">No projects yet</p>
          <p className="text-text-dim text-xs mt-2">
            Create a new project or click Scan to auto-detect projects in <span className="font-mono text-accent">{projectsRoot || '...'}</span>
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {projects.map(p => (
            <div
              key={p.name}
              className="flex items-center justify-between px-4 py-3 bg-bg-secondary rounded-lg border border-border hover:border-text-dim transition-colors"
            >
              <div className="flex items-center gap-4 min-w-0">
                <div className={`w-2 h-2 rounded-full ${p.ssl_enabled ? 'bg-status-green' : 'bg-text-dim'}`} />
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{p.name}</span>
                    <span className={`text-xs capitalize ${frameworkBadge[p.framework] || 'text-text-dim'}`}>
                      {p.framework}
                    </span>
                  </div>
                  <div className="flex items-center gap-3 mt-0.5">
                    <span className="text-xs text-accent font-mono">{p.domain}</span>
                    <span className="text-xs text-text-dim font-mono truncate">{p.path}</span>
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-2 flex-shrink-0">
                {phpVersions.length > 0 ? (
                  <select
                    value={p.php_version || ''}
                    onChange={e => handleChangePHP(p.name, e.target.value)}
                    className="px-2 py-1 bg-bg-primary border border-border rounded text-xs font-mono text-text-muted focus:outline-none focus:border-accent/50 cursor-pointer"
                  >
                    {phpVersions.map(v => (
                      <option key={v.version} value={v.version}>PHP {v.version}</option>
                    ))}
                  </select>
                ) : (
                  p.php_version && <span className="text-xs text-text-dim font-mono">PHP {p.php_version}</span>
                )}
                <span className="text-xs text-text-dim">{p.web_server}</span>
                <button
                  onClick={() => handleToggleSSL(p)}
                  className={`p-1.5 transition-colors ${p.ssl_enabled ? 'text-status-green hover:text-status-green/70' : 'text-text-dim hover:text-text-primary'}`}
                  title={p.ssl_enabled ? 'HTTPS enabled - click to disable' : 'HTTPS disabled - click to enable'}
                >
                  {p.ssl_enabled ? <Lock size={12} /> : <Unlock size={12} />}
                </button>
                <button
                  onClick={() => handleOpenBrowser(p.domain, p.ssl_enabled)}
                  className="p-1.5 text-text-dim hover:text-text-primary transition-colors"
                  title="Open in browser"
                >
                  <ExternalLink size={12} />
                </button>
                <button
                  onClick={() => handleDelete(p.name)}
                  className="p-1.5 text-text-dim hover:text-status-red transition-colors"
                  title="Delete project"
                >
                  <Trash2 size={12} />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
