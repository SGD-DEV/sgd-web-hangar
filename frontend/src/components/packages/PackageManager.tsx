import { useState, useEffect, useCallback } from 'react'
import { Download, Check, Trash2, Loader2, Package, Zap, AlertCircle, Plus, Link as LinkIcon, ExternalLink } from 'lucide-react'

interface CategoryInfo {
  id: string
  label: string
  description: string
}

interface PackageInfo {
  name: string
  label: string
  version: string
  category: string
  status: 'available' | 'downloading' | 'installed' | 'active' | 'error'
  install_path: string
  is_active: boolean
}

interface DownloadProgress {
  name: string
  version: string
  percent: number
  downloaded: number
  total: number
  speed_mbps: number
  eta_seconds: number
  status: string
  error: string
}

const categoryIcons: Record<string, string> = {
  php: 'PHP',
  webserver: 'WEB',
  database: 'DB',
  search: 'SRCH',
  nodejs: 'JS',
  python: 'PY',
  tools: 'TOOL',
  cloud: 'CLD',
  golang: 'GO',
}

const statusColors: Record<string, string> = {
  available: 'text-text-muted',
  downloading: 'text-status-yellow',
  installed: 'text-accent',
  active: 'text-status-green',
  error: 'text-status-red',
}

export default function PackageManager() {
  const [categories, setCategories] = useState<CategoryInfo[]>([])
  const [activeCategory, setActiveCategory] = useState<string>('all')
  const [packages, setPackages] = useState<PackageInfo[]>([])
  const [progress, setProgress] = useState<Record<string, DownloadProgress>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>('')
  const [showAddCustom, setShowAddCustom] = useState(false)

  const loadData = useCallback(async () => {
    try {
      if (window.go?.app?.App?.GetPackageCategories) {
        const cats = await window.go.app.App.GetPackageCategories()
        setCategories(cats || [])
      }
      if (window.go?.app?.App?.GetPackages) {
        const pkgs = await window.go.app.App.GetPackages(activeCategory === 'all' ? '' : activeCategory)
        setPackages(pkgs || [])
      }
      if (window.go?.app?.App?.GetAllDownloadProgress) {
        const prog = await window.go.app.App.GetAllDownloadProgress()
        setProgress(prog || {})
      }
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }, [activeCategory])

  useEffect(() => {
    loadData()
    const interval = setInterval(loadData, 2000)
    return () => clearInterval(interval)
  }, [loadData])

  // Listen for real-time progress events
  useEffect(() => {
    const handler = (data: DownloadProgress) => {
      setProgress(prev => ({
        ...prev,
        [data.name + '-' + data.version]: data,
      }))
      // Reload packages when a download completes
      if (data.status === 'complete' || data.status === 'error') {
        setTimeout(loadData, 500)
      }
    }

    // @ts-ignore - Wails runtime events
    if (window.runtime?.EventsOn) {
      // @ts-ignore
      window.runtime.EventsOn('package:progress', handler)
      // @ts-ignore
      return () => window.runtime?.EventsOff?.('package:progress')
    }
  }, [loadData])

  async function handleInstall(name: string, version: string) {
    setError('')
    try {
      await window.go.app.App.InstallPackage(name, version)
    } catch (e: any) {
      setError(e?.message || String(e))
    }
  }

  async function handleActivate(name: string, version: string) {
    setError('')
    try {
      await window.go.app.App.ActivatePackage(name, version)
      await loadData()
    } catch (e: any) {
      setError(e?.message || String(e))
    }
  }

  async function handleRemove(name: string, version: string) {
    setError('')
    try {
      await window.go.app.App.RemovePackage(name, version)
      await loadData()
    } catch (e: any) {
      setError(e?.message || String(e))
    }
  }

  // Tools that have a real launch action. heidisql gets a session-loaded
  // launch via OpenHeidiSQL; the rest go through the generic LaunchTool
  // wrapper that just exec's the package's main exe.
  const launchablePackages = ['heidisql', 'dbeaver', 'vscode', 'pocketbase', 'phpmyadmin']

  async function handleOpen(name: string) {
    setError('')
    try {
      if (name === 'heidisql') {
        // @ts-ignore Wails bindings
        await window.go?.app?.App?.OpenHeidiSQL?.()
        return
      }
      if (name === 'phpmyadmin') {
        // @ts-ignore Wails bindings - opens the user's default browser to
        // the auto-served phpMyAdmin URL. If a vhost isn't set up yet the
        // backend writes one and starts Apache.
        const url = await window.go?.app?.App?.OpenPhpMyAdmin?.()
        if (url) {
          // Backend returns the URL; we ask Wails to open it externally.
          // @ts-ignore
          await window.runtime?.BrowserOpenURL?.(url)
        }
        return
      }
      // @ts-ignore Wails bindings
      await window.go?.app?.App?.LaunchTool?.(name, '')
    } catch (e: any) {
      setError(e?.message || String(e))
    }
  }

  function getProgress(name: string, version: string): DownloadProgress | null {
    const key = name + '-' + version
    return progress[key] || null
  }

  function formatBytes(bytes: number): string {
    if (bytes >= 1024 * 1024 * 1024) return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB'
    if (bytes >= 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
    if (bytes >= 1024) return (bytes / 1024).toFixed(1) + ' KB'
    return bytes + ' B'
  }

  return (
    <div className="flex-1 flex h-full overflow-hidden">
      {/* Category sidebar */}
      <div className="w-48 border-r border-border flex-shrink-0 overflow-y-auto">
        <div className="p-4">
          <h2 className="text-xs font-medium text-text-muted uppercase tracking-wider mb-3">Categories</h2>
          <div className="space-y-0.5">
            <button
              onClick={() => setActiveCategory('all')}
              className={`w-full flex items-center gap-2 px-3 py-2 rounded-lg text-xs transition-colors
                ${activeCategory === 'all'
                  ? 'bg-bg-selected text-accent'
                  : 'text-text-muted hover:text-text-primary hover:bg-bg-secondary/50'
                }`}
            >
              <Package size={12} />
              All Packages
            </button>
            {categories.map(cat => (
              <button
                key={cat.id}
                onClick={() => setActiveCategory(cat.id)}
                className={`w-full flex items-center gap-2 px-3 py-2 rounded-lg text-xs transition-colors
                  ${activeCategory === cat.id
                    ? 'bg-bg-selected text-accent'
                    : 'text-text-muted hover:text-text-primary hover:bg-bg-secondary/50'
                  }`}
              >
                <span className="text-[10px] font-bold font-mono w-5 text-center opacity-60">
                  {categoryIcons[cat.id] || '?'}
                </span>
                {cat.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Package list */}
      <div className="flex-1 overflow-y-auto p-6">
        <div className="flex items-center justify-between mb-6">
          <div>
            <h2 className="text-lg font-medium">Packages</h2>
            <p className="text-xs text-text-muted mt-1">
              Download and manage development tools — {packages.length} packages available
            </p>
          </div>
          <button
            onClick={() => setShowAddCustom(s => !s)}
            className="flex items-center gap-2 px-3 py-1.5 bg-accent text-on-accent rounded-lg text-xs font-medium hover:bg-accent-hover transition-colors"
            title="Add a package by URL (e.g. a new PHP release)"
          >
            <Plus size={12} />
            Add by URL
          </button>
        </div>

        {showAddCustom && <CustomPackageForm onClose={() => setShowAddCustom(false)} onAdded={() => { loadData(); setShowAddCustom(false) }} />}

        {error && (
          <div className="mb-4 flex items-center gap-2 px-4 py-3 bg-status-red/10 border border-status-red/20 rounded-lg text-xs text-status-red">
            <AlertCircle size={14} />
            {error}
            <button onClick={() => setError('')} className="ml-auto text-status-red/60 hover:text-status-red">×</button>
          </div>
        )}

        {loading ? (
          <div className="text-text-dim text-sm flex items-center gap-2">
            <Loader2 size={14} className="animate-spin" />
            Loading packages...
          </div>
        ) : packages.length === 0 ? (
          <div className="bg-bg-secondary rounded-lg p-8 border border-border text-center">
            <Package size={32} className="mx-auto text-text-dim mb-3" />
            <p className="text-text-muted text-sm">No packages in this category</p>
          </div>
        ) : (
          <div className="space-y-2">
            {packages.map(pkg => {
              const prog = getProgress(pkg.name, pkg.version)
              const isDownloading = pkg.status === 'downloading' || (prog && prog.status === 'downloading')

              return (
                <div
                  key={pkg.name + '-' + pkg.version}
                  className="flex items-center justify-between px-4 py-3.5 bg-bg-secondary rounded-lg border border-border hover:border-text-dim transition-colors"
                >
                  <div className="flex items-center gap-4 min-w-0 flex-1">
                    <div className={`w-8 h-8 rounded-lg flex items-center justify-center text-[10px] font-bold font-mono
                      ${pkg.status === 'active' ? 'bg-status-green/10 text-status-green' :
                        pkg.status === 'installed' ? 'bg-accent/10 text-accent' :
                        'bg-bg-primary text-text-dim'} border border-border`}>
                      {categoryIcons[pkg.category] || '?'}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium">{pkg.label}</span>
                        {pkg.is_active && (
                          <span className="flex items-center gap-1 text-[10px] text-status-green font-medium px-1.5 py-0.5 bg-status-green/10 rounded">
                            <Zap size={8} />
                            Active
                          </span>
                        )}
                        {pkg.status === 'installed' && !pkg.is_active && (
                          <span className="text-[10px] text-accent font-medium px-1.5 py-0.5 bg-accent/10 rounded">
                            Installed
                          </span>
                        )}
                      </div>
                      <div className="flex items-center gap-3 mt-0.5">
                        <span className="text-xs text-text-dim font-mono">{pkg.name} v{pkg.version}</span>
                        {pkg.install_path && (
                          <span className="text-xs text-text-dim font-mono truncate max-w-[300px]">{pkg.install_path}</span>
                        )}
                      </div>

                      {/* Download progress bar */}
                      {isDownloading && prog && (
                        <div className="mt-2 space-y-1">
                          <div className="w-full h-1.5 bg-bg-primary rounded-full overflow-hidden">
                            <div
                              className="h-full bg-accent rounded-full transition-all duration-300"
                              style={{ width: `${Math.min(prog.percent, 100)}%` }}
                            />
                          </div>
                          <div className="flex items-center justify-between text-[10px] text-text-dim">
                            <span>{prog.status === 'extracting' ? 'Extracting...' : `${prog.percent.toFixed(0)}%`}</span>
                            <span>
                              {prog.total > 0 ? `${formatBytes(prog.downloaded)} / ${formatBytes(prog.total)}` : ''}
                              {prog.speed_mbps > 0 ? ` • ${prog.speed_mbps.toFixed(1)} MB/s` : ''}
                            </span>
                          </div>
                        </div>
                      )}

                      {/* Error state */}
                      {prog?.status === 'error' && (
                        <div className="mt-1 text-[10px] text-status-red">{prog.error}</div>
                      )}
                    </div>
                  </div>

                  {/* Actions */}
                  <div className="flex items-center gap-2 flex-shrink-0 ml-4">
                    {pkg.status === 'available' && (
                      <button
                        onClick={() => handleInstall(pkg.name, pkg.version)}
                        className="flex items-center gap-1.5 px-3 py-1.5 bg-accent text-on-accent rounded-lg text-xs font-medium hover:bg-accent-hover transition-colors"
                      >
                        <Download size={12} />
                        Install
                      </button>
                    )}

                    {isDownloading && (
                      <button
                        disabled
                        className="flex items-center gap-1.5 px-3 py-1.5 bg-bg-primary text-text-dim rounded-lg text-xs border border-border cursor-not-allowed"
                      >
                        <Loader2 size={12} className="animate-spin" />
                        Installing...
                      </button>
                    )}

                    {pkg.status === 'installed' && !pkg.is_active && (
                      <>
                        {launchablePackages.includes(pkg.name) && (
                          <button
                            onClick={() => handleOpen(pkg.name)}
                            className="flex items-center gap-1.5 px-3 py-1.5 bg-accent/10 text-accent border border-accent/20 rounded-lg text-xs font-medium hover:bg-accent/20 transition-colors"
                            title="Launch / open this tool"
                          >
                            <ExternalLink size={12} />
                            Open
                          </button>
                        )}
                        {!launchablePackages.includes(pkg.name) && (
                          <button
                            onClick={() => handleActivate(pkg.name, pkg.version)}
                            className="flex items-center gap-1.5 px-3 py-1.5 bg-accent/10 text-accent border border-accent/20 rounded-lg text-xs font-medium hover:bg-accent/20 transition-colors"
                          >
                            <Zap size={12} />
                            Activate
                          </button>
                        )}
                        <button
                          onClick={() => handleRemove(pkg.name, pkg.version)}
                          className="p-1.5 text-text-dim hover:text-status-red transition-colors"
                          title="Remove"
                        >
                          <Trash2 size={14} />
                        </button>
                      </>
                    )}

                    {pkg.status === 'active' && (
                      <span className="flex items-center gap-1 px-3 py-1.5 text-xs text-status-green">
                        <Check size={12} />
                        Active
                      </span>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

// CustomPackageForm lets the user paste a download URL for a new package
// version (e.g. "PHP 8.5 just released, here's the URL"). The URL goes to
// the backend's URLDetect which sniffs the host (windows.php.net,
// dev.mysql.com, github.com, etc.) and prefills name/version/category, so
// the user just clicks Save.
//
// On Save, the entry is added to the persistent custom-packages store and
// the parent's onAdded callback refreshes the package list - the new entry
// then appears in the normal "Available" list with an Install button.
interface DetectedPackage {
  name: string
  version: string
  label: string
  category: string
  sub_dir: string
  confidence: 'high' | 'medium' | 'low' | 'unknown' | string
  note?: string
}

function CustomPackageForm({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const [url, setUrl] = useState('')
  const [detected, setDetected] = useState<DetectedPackage | null>(null)
  const [name, setName] = useState('')
  const [version, setVersion] = useState('')
  const [label, setLabel] = useState('')
  const [category, setCategory] = useState('tools')
  const [subDir, setSubDir] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  // Debounce URL detection so we don't hammer the backend on every keystroke.
  useEffect(() => {
    if (!url.trim()) { setDetected(null); return }
    const handle = setTimeout(async () => {
      try {
        // @ts-ignore Wails bindings
        const d = await window.go?.app?.App?.DetectPackageURL?.(url.trim())
        if (d) {
          setDetected(d)
          // Only overwrite fields the user hasn't typed into. Once they
          // start editing, respect their input.
          setName(prev => prev || d.name || '')
          setVersion(prev => prev || d.version || '')
          setLabel(prev => prev || d.label || '')
          setCategory(prev => prev || d.category || 'tools')
          setSubDir(prev => prev || d.sub_dir || '')
        }
      } catch { /* ignore - user can fill manually */ }
    }, 300)
    return () => clearTimeout(handle)
  }, [url])

  async function handleSave() {
    setError('')
    if (!url.trim() || !name.trim() || !version.trim()) {
      setError('URL, Name and Version are required.')
      return
    }
    setSubmitting(true)
    try {
      // @ts-ignore Wails bindings
      await window.go?.app?.App?.AddCustomPackage?.({
        name: name.trim(),
        label: label.trim() || `${name.trim()} ${version.trim()}`,
        version: version.trim(),
        url: url.trim(),
        category,
        sub_dir: subDir.trim() || `${name.trim()}/${version.trim()}`,
      })
      onAdded()
    } catch (e: any) {
      setError(e?.message || String(e))
    } finally {
      setSubmitting(false)
    }
  }

  const confidenceColor: Record<string, string> = {
    high: 'text-status-green',
    medium: 'text-status-yellow',
    low: 'text-status-yellow',
    unknown: 'text-text-muted',
  }

  return (
    <div className="mb-6 bg-bg-secondary border border-border rounded-lg p-4 space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium flex items-center gap-2">
          <LinkIcon size={14} /> Add Package by URL
        </h3>
        <button onClick={onClose} className="text-text-muted hover:text-text-primary text-sm">×</button>
      </div>
      <p className="text-xs text-text-muted">
        Paste a download URL (e.g. for a new PHP release). We'll auto-detect the package type from
        the host. Edit any field below before saving.
      </p>

      <div>
        <label className="text-xs text-text-muted block mb-1">Download URL</label>
        <input
          type="url"
          value={url}
          onChange={e => setUrl(e.target.value)}
          placeholder="https://windows.php.net/downloads/releases/archives/php-8.5.0-nts-Win32-vs17-x64.zip"
          className="w-full bg-bg-primary border border-border rounded px-3 py-2 text-xs font-mono focus:border-accent focus:outline-none"
        />
        {detected && (
          <p className={`text-[11px] mt-1 ${confidenceColor[detected.confidence] || 'text-text-muted'}`}>
            Detected: {detected.label} ({detected.confidence} confidence)
            {detected.note ? ' — ' + detected.note : ''}
          </p>
        )}
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="text-xs text-text-muted block mb-1">Name</label>
          <input
            value={name}
            onChange={e => setName(e.target.value)}
            placeholder="php"
            className="w-full bg-bg-primary border border-border rounded px-3 py-1.5 text-xs font-mono"
          />
        </div>
        <div>
          <label className="text-xs text-text-muted block mb-1">Version</label>
          <input
            value={version}
            onChange={e => setVersion(e.target.value)}
            placeholder="8.5"
            className="w-full bg-bg-primary border border-border rounded px-3 py-1.5 text-xs font-mono"
          />
        </div>
      </div>

      <div>
        <label className="text-xs text-text-muted block mb-1">Label (display name)</label>
        <input
          value={label}
          onChange={e => setLabel(e.target.value)}
          placeholder="PHP 8.5"
          className="w-full bg-bg-primary border border-border rounded px-3 py-1.5 text-xs"
        />
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="text-xs text-text-muted block mb-1">Category</label>
          <select
            value={category}
            onChange={e => setCategory(e.target.value)}
            className="w-full bg-bg-primary border border-border rounded px-3 py-1.5 text-xs"
          >
            <option value="php">PHP</option>
            <option value="webserver">Web Server</option>
            <option value="database">Database</option>
            <option value="nodejs">Node.js</option>
            <option value="tools">Tools</option>
            <option value="golang">Go</option>
          </select>
        </div>
        <div>
          <label className="text-xs text-text-muted block mb-1">Install subfolder</label>
          <input
            value={subDir}
            onChange={e => setSubDir(e.target.value)}
            placeholder="php/8.5"
            className="w-full bg-bg-primary border border-border rounded px-3 py-1.5 text-xs font-mono"
          />
        </div>
      </div>

      {error && (
        <div className="flex items-center gap-2 text-xs text-status-red">
          <AlertCircle size={12} /> {error}
        </div>
      )}

      <div className="flex justify-end gap-2 pt-1">
        <button
          onClick={onClose}
          className="px-3 py-1.5 text-xs text-text-muted hover:text-text-primary transition-colors"
        >
          Cancel
        </button>
        <button
          onClick={handleSave}
          disabled={submitting}
          className="flex items-center gap-1.5 px-4 py-1.5 bg-accent text-on-accent rounded text-xs font-medium hover:bg-accent-hover transition-colors disabled:opacity-50"
        >
          {submitting ? <Loader2 size={12} className="animate-spin" /> : <Plus size={12} />}
          {submitting ? 'Adding...' : 'Add Package'}
        </button>
      </div>
    </div>
  )
}
