import { useCallback, useEffect, useState } from 'react'
import { Check, AlertCircle, Trash2, Plus, RotateCcw, Info } from 'lucide-react'

interface PathEntry {
  path: string
  scope: 'user' | 'system'
  managed: boolean
  runtime?: string
  version?: string
  exists: boolean
}

interface RuntimeInfo {
  name: string
  label: string
  active_version: string
  active_path: string
  installed_versions: string[]
}

export default function SystemPathManager() {
  const [entries, setEntries] = useState<PathEntry[]>([])
  const [runtimes, setRuntimes] = useState<RuntimeInfo[]>([])
  const [error, setError] = useState<string>('')
  const [notice, setNotice] = useState<string>('')
  const [loading, setLoading] = useState<boolean>(true)
  const [newPath, setNewPath] = useState<string>('')

  const load = useCallback(async () => {
    setError('')
    try {
      // @ts-ignore Wails bindings
      const e: PathEntry[] = (await window.go?.app?.App?.GetSystemPathEntries?.()) || []
      // @ts-ignore Wails bindings
      const r: RuntimeInfo[] = (await window.go?.app?.App?.GetSystemPathRuntimes?.()) || []
      setEntries(e)
      setRuntimes(r)
    } catch (err) {
      setError(String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  async function handleSetActive(runtime: string, version: string) {
    if (!version) return
    setError('')
    setNotice('')
    try {
      // @ts-ignore
      await window.go.app.App.SetActiveRuntimeOnPath(runtime, version)
      setNotice(`${runtime} ${version} is now on PATH. Open a new terminal to pick up the change.`)
      await load()
    } catch (err) {
      setError(String(err))
    }
  }

  async function handleRemove(entry: PathEntry) {
    if (entry.scope === 'system') {
      setError('System-scope entries require admin elevation. Use Windows Settings → Environment Variables to remove these.')
      return
    }
    setError('')
    setNotice('')
    try {
      // @ts-ignore
      await window.go.app.App.RemoveSystemPathEntry(entry.path, entry.scope)
      setNotice('Removed. Open a new terminal to see the change.')
      await load()
    } catch (err) {
      setError(String(err))
    }
  }

  async function handleAdd() {
    if (!newPath.trim()) return
    setError('')
    setNotice('')
    try {
      // @ts-ignore
      await window.go.app.App.AddSystemPathEntry(newPath.trim())
      setNewPath('')
      setNotice('Added to user PATH. Open a new terminal to see the change.')
      await load()
    } catch (err) {
      setError(String(err))
    }
  }

  const userEntries = entries.filter(e => e.scope === 'user')
  const systemEntries = entries.filter(e => e.scope === 'system')

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="max-w-4xl">
        <div className="flex items-center justify-between mb-2">
          <h2 className="text-lg font-medium">System PATH</h2>
          <button
            onClick={load}
            className="text-xs text-text-muted hover:text-text-primary flex items-center gap-1.5 px-2 py-1 rounded hover:bg-bg-secondary transition-colors"
          >
            <RotateCcw size={12} /> Refresh
          </button>
        </div>
        <p className="text-sm text-text-muted mb-6">
          Switch which version of PHP, Node, MySQL, etc. is on your PATH without
          opening Windows Environment Variables. Changes write to your <strong className="text-text-primary">user</strong> PATH (no admin
          needed) and broadcast immediately — open a new terminal to see them.
        </p>

        {error && (
          <div className="mb-4 flex items-start gap-2 px-3 py-2 bg-status-red/10 border border-status-red/30 rounded text-sm text-status-red">
            <AlertCircle size={16} className="flex-shrink-0 mt-0.5" />
            <span>{error}</span>
          </div>
        )}
        {notice && (
          <div className="mb-4 flex items-start gap-2 px-3 py-2 bg-accent/10 border border-accent/30 rounded text-sm text-accent">
            <Check size={16} className="flex-shrink-0 mt-0.5" />
            <span>{notice}</span>
          </div>
        )}

        {/* Runtime quick-switch */}
        <div className="mb-8">
          <h3 className="text-sm font-medium mb-3">Active runtimes</h3>
          {loading ? (
            <p className="text-xs text-text-muted">Loading…</p>
          ) : runtimes.length === 0 ? (
            <p className="text-xs text-text-muted">No runtimes installed yet. Install some from the Packages page.</p>
          ) : (
            <div className="space-y-2">
              {runtimes.map(rt => {
                const installed = rt.installed_versions || []
                const noneInstalled = installed.length === 0
                return (
                  <div
                    key={rt.name}
                    className="flex items-center gap-3 p-3 border border-border rounded bg-bg-secondary"
                  >
                    <div className="w-32 flex-shrink-0">
                      <div className="text-sm font-medium text-text-primary">{rt.label}</div>
                      <div className="text-xs text-text-muted">{rt.name}</div>
                    </div>
                    <div className="flex-1 min-w-0">
                      {rt.active_version ? (
                        <div className="text-xs">
                          <span className="text-status-green">● on PATH:</span>{' '}
                          <span className="text-text-primary font-mono">{rt.active_version}</span>
                          <div className="text-text-dim font-mono truncate" title={rt.active_path}>
                            {rt.active_path}
                          </div>
                        </div>
                      ) : (
                        <div className="text-xs text-text-muted">Not on PATH</div>
                      )}
                    </div>
                    <div className="flex-shrink-0">
                      {noneInstalled ? (
                        <span className="text-xs text-text-dim italic">no versions installed</span>
                      ) : (
                        <select
                          value={rt.active_version || ''}
                          onChange={e => handleSetActive(rt.name, e.target.value)}
                          className="px-2 py-1.5 bg-bg-primary border border-border rounded text-xs text-text-primary focus:outline-none focus:border-accent/50"
                        >
                          <option value="" disabled>
                            Choose…
                          </option>
                          {installed.map(v => (
                            <option key={v} value={v}>
                              {v}
                            </option>
                          ))}
                        </select>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>

        {/* User PATH list */}
        <div className="mb-6">
          <h3 className="text-sm font-medium mb-3 flex items-center gap-2">
            User PATH
            <span className="text-xs font-normal text-text-dim">(HKCU\Environment, no admin needed)</span>
          </h3>

          <div className="flex gap-2 mb-3">
            <input
              type="text"
              value={newPath}
              onChange={e => setNewPath(e.target.value)}
              placeholder={`C:\\path\\to\\add`}
              className="flex-1 px-3 py-2 bg-bg-secondary border border-border rounded text-sm font-mono text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50"
            />
            <button
              onClick={handleAdd}
              disabled={!newPath.trim()}
              className="px-3 py-2 bg-accent text-on-accent rounded text-sm font-medium hover:bg-accent/90 transition-colors disabled:opacity-40 flex items-center gap-1.5"
            >
              <Plus size={14} /> Add
            </button>
          </div>

          <PathList entries={userEntries} onRemove={handleRemove} removable />
        </div>

        {/* System PATH (read-only) */}
        <div>
          <h3 className="text-sm font-medium mb-3 flex items-center gap-2">
            System PATH
            <span className="text-xs font-normal text-text-dim">(HKLM, read-only — edit via Windows Settings)</span>
          </h3>
          <PathList entries={systemEntries} onRemove={handleRemove} removable={false} />
        </div>

        <div className="mt-8 flex items-start gap-2 text-xs text-text-dim border-t border-border pt-4">
          <Info size={12} className="mt-0.5 flex-shrink-0" />
          <span>
            User PATH wins over System PATH for your shells, so you don&apos;t need admin
            access to override a system-installed PHP. Already-open terminals
            won&apos;t see new entries — close and reopen.
          </span>
        </div>
      </div>
    </div>
  )
}

function PathList({
  entries,
  onRemove,
  removable,
}: {
  entries: PathEntry[]
  onRemove: (e: PathEntry) => void
  removable: boolean
}) {
  if (entries.length === 0) {
    return <p className="text-xs text-text-muted italic">No entries.</p>
  }
  return (
    <div className="border border-border rounded divide-y divide-border bg-bg-secondary">
      {entries.map((e, i) => (
        <div key={`${e.path}-${i}`} className="flex items-center gap-2 px-3 py-2 text-xs">
          <span className="flex-shrink-0 w-4">
            {e.managed ? (
              <span className="text-accent" title="Hangar-managed">
                ◆
              </span>
            ) : null}
          </span>
          <code
            className={`flex-1 font-mono truncate ${
              e.exists ? 'text-text-primary' : 'text-text-dim line-through'
            }`}
            title={e.path}
          >
            {e.path}
          </code>
          {e.runtime && (
            <span className="text-xs text-accent bg-accent/10 px-1.5 py-0.5 rounded">
              {e.runtime}
              {e.version ? ` ${e.version}` : ''}
            </span>
          )}
          {!e.exists && (
            <span className="text-xs text-status-yellow" title="path does not exist on disk">
              missing
            </span>
          )}
          {removable && (
            <button
              onClick={() => onRemove(e)}
              className="text-text-muted hover:text-status-red transition-colors p-1 rounded hover:bg-bg-primary"
              title="Remove from PATH"
            >
              <Trash2 size={12} />
            </button>
          )}
        </div>
      ))}
    </div>
  )
}
