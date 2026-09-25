import { useState, useEffect, useRef } from 'react'
import { Check, Download, Settings, Package, Search, X } from 'lucide-react'

interface PHPVersion {
  version: string
  path: string
  is_active: boolean
  is_default: boolean
}

interface Extension {
  name: string
  enabled: boolean
}

interface PHPVersionListProps {
  onNavigate?: (nav: string) => void
}

export default function PHPVersionList({ onNavigate }: PHPVersionListProps) {
  const [versions, setVersions] = useState<PHPVersion[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedVersion, setSelectedVersion] = useState<string>('')
  const [extensions, setExtensions] = useState<Extension[]>([])
  const [extLoading, setExtLoading] = useState(false)
  const [activeTab, setActiveTab] = useState<'extensions' | 'ini'>('extensions')
  const [iniContent, setIniContent] = useState('')
  const [iniSaving, setIniSaving] = useState(false)
  const [iniSaved, setIniSaved] = useState(false)
  const [extFilter, setExtFilter] = useState('')
  const [iniSearch, setIniSearch] = useState('')
  const [iniSearchVisible, setIniSearchVisible] = useState(false)
  const [iniMatchCount, setIniMatchCount] = useState(0)
  const iniTextareaRef = useRef<HTMLTextAreaElement>(null)
  const iniSearchRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    loadVersions()
  }, [])

  // Ctrl+F to open search in ini tab
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if ((e.ctrlKey || e.metaKey) && e.key === 'f' && activeTab === 'ini') {
        e.preventDefault()
        setIniSearchVisible(true)
        setTimeout(() => iniSearchRef.current?.focus(), 50)
      }
      if (e.key === 'Escape' && iniSearchVisible) {
        setIniSearchVisible(false)
        setIniSearch('')
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [activeTab, iniSearchVisible])

  // Count search matches
  useEffect(() => {
    if (!iniSearch || !iniContent) { setIniMatchCount(0); return }
    const regex = new RegExp(iniSearch.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi')
    const matches = iniContent.match(regex)
    setIniMatchCount(matches?.length || 0)
  }, [iniSearch, iniContent])

  useEffect(() => {
    if (selectedVersion) {
      loadExtensions(selectedVersion)
    }
  }, [selectedVersion])

  async function loadVersions() {
    try {
      if (window.go?.app?.App?.GetInstalledPHPVersions) {
        const v = await window.go.app.App.GetInstalledPHPVersions()
        setVersions(v || [])
        const active = v?.find((x: PHPVersion) => x.is_active)
        if (active && !selectedVersion) setSelectedVersion(active.version)
      }
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }

  async function loadExtensions(version: string) {
    setExtLoading(true)
    try {
      const exts = await window.go?.app?.App?.GetPHPExtensions?.(version)
      setExtensions(exts || [])
    } catch (e) {
      console.error(e)
    } finally {
      setExtLoading(false)
    }
  }

  async function loadIni(version: string) {
    try {
      const content = await window.go?.app?.App?.ReadConfigFile?.('php', 'php.ini')
      setIniContent(content || '; No php.ini found — start Apache to auto-generate one')
    } catch (e: any) {
      setIniContent('; Error loading php.ini: ' + (e?.message || String(e)))
    }
  }

  async function handleSwitch(version: string) {
    try {
      await window.go?.app?.App?.SwitchPHP?.(version)
      await loadVersions()
      setSelectedVersion(version)
    } catch (e) {
      console.error(e)
    }
  }

  async function handleToggleExt(extName: string, currentEnabled: boolean) {
    try {
      await window.go?.app?.App?.TogglePHPExtension?.(selectedVersion, extName, !currentEnabled)
      await loadExtensions(selectedVersion)
    } catch (e) {
      console.error(e)
    }
  }

  async function handleSaveIni() {
    setIniSaving(true)
    setIniSaved(false)
    try {
      await window.go?.app?.App?.WriteConfigFile?.('php', 'php.ini', iniContent)
      setIniSaved(true)
      setTimeout(() => setIniSaved(false), 2000)
    } catch (e) {
      console.error(e)
    } finally {
      setIniSaving(false)
    }
  }

  const filteredExts = extensions.filter(e =>
    !extFilter || e.name.toLowerCase().includes(extFilter.toLowerCase())
  )

  return (
    <div className="flex-1 flex overflow-hidden">
      {/* Left: Version list */}
      <div className="w-72 border-r border-border flex-shrink-0 overflow-y-auto p-4">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-xs font-medium text-text-muted uppercase tracking-wider">PHP Versions</h2>
          <button
            onClick={() => onNavigate?.('packages')}
            className="p-1 text-text-dim hover:text-text-primary transition-colors"
            title="Install new version"
          >
            <Download size={14} />
          </button>
        </div>

        {loading ? (
          <div className="text-text-dim text-sm">Loading...</div>
        ) : versions.length === 0 ? (
          <div className="text-text-dim text-xs text-center py-4">
            No PHP versions installed
          </div>
        ) : (
          <div className="space-y-1">
            {versions.map(v => (
              <button
                key={v.version}
                onClick={() => setSelectedVersion(v.version)}
                className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-all text-left
                  ${selectedVersion === v.version
                    ? 'bg-bg-selected border border-accent/20'
                    : 'hover:bg-bg-secondary border border-transparent'
                  }`}
              >
                <div className={`w-2 h-2 rounded-full flex-shrink-0 ${v.is_active ? 'bg-status-green' : 'bg-text-dim'}`} />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium font-mono">PHP {v.version}</span>
                    {v.is_active && <Check size={12} className="text-accent" />}
                  </div>
                  {!v.is_active && (
                    <button
                      onClick={(e) => { e.stopPropagation(); handleSwitch(v.version) }}
                      className="text-xs text-text-dim hover:text-accent transition-colors mt-0.5"
                    >
                      Activate
                    </button>
                  )}
                </div>
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Right: Extensions & Settings */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {selectedVersion ? (
          <>
            {/* Tabs */}
            <div className="border-b border-border px-6 pt-4 flex items-center gap-4">
              <button
                onClick={() => setActiveTab('extensions')}
                className={`pb-2 text-sm font-medium border-b-2 transition-colors ${activeTab === 'extensions' ? 'border-accent text-text-primary' : 'border-transparent text-text-muted hover:text-text-primary'}`}
              >
                <span className="flex items-center gap-1.5"><Package size={13} /> Extensions</span>
              </button>
              <button
                onClick={() => { setActiveTab('ini'); loadIni(selectedVersion) }}
                className={`pb-2 text-sm font-medium border-b-2 transition-colors ${activeTab === 'ini' ? 'border-accent text-text-primary' : 'border-transparent text-text-muted hover:text-text-primary'}`}
              >
                <span className="flex items-center gap-1.5"><Settings size={13} /> php.ini</span>
              </button>
            </div>

            {activeTab === 'extensions' && (
              <div className="flex-1 overflow-y-auto p-6">
                <div className="flex items-center justify-between mb-4">
                  <div>
                    <h3 className="text-sm font-medium">Extensions for PHP {selectedVersion}</h3>
                    <p className="text-xs text-text-muted mt-0.5">{extensions.filter(e => e.enabled).length} of {extensions.length} enabled</p>
                  </div>
                  <input
                    type="text"
                    value={extFilter}
                    onChange={e => setExtFilter(e.target.value)}
                    placeholder="Filter..."
                    className="px-3 py-1.5 w-48 bg-bg-secondary border border-border rounded-lg text-xs text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50 font-mono"
                  />
                </div>

                {extLoading ? (
                  <div className="text-text-dim text-sm">Loading extensions...</div>
                ) : filteredExts.length === 0 ? (
                  <div className="text-text-dim text-xs text-center py-8">No extensions found</div>
                ) : (
                  <div className="grid grid-cols-2 gap-1.5">
                    {filteredExts.map(ext => (
                      <button
                        key={ext.name}
                        onClick={() => handleToggleExt(ext.name, ext.enabled)}
                        className={`flex items-center gap-2.5 px-3 py-2 rounded-lg border text-left transition-colors
                          ${ext.enabled
                            ? 'bg-accent/5 border-accent/20 hover:border-accent/40'
                            : 'bg-bg-secondary border-border hover:border-text-dim'
                          }`}
                      >
                        <div className={`w-3.5 h-3.5 rounded border flex items-center justify-center flex-shrink-0 transition-colors
                          ${ext.enabled ? 'bg-accent border-accent' : 'border-text-dim bg-transparent'}`}
                        >
                          {ext.enabled && <Check size={10} className="text-on-accent" />}
                        </div>
                        <span className={`text-xs font-mono truncate ${ext.enabled ? 'text-text-primary' : 'text-text-muted'}`}>
                          {ext.name}
                        </span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}

            {activeTab === 'ini' && (
              <div className="flex-1 flex flex-col overflow-hidden p-6">
                <div className="flex items-center justify-between mb-3">
                  <h3 className="text-sm font-medium">php.ini for PHP {selectedVersion}</h3>
                  <div className="flex items-center gap-2">
                    <button
                      onClick={() => { setIniSearchVisible(!iniSearchVisible); setTimeout(() => iniSearchRef.current?.focus(), 50) }}
                      className="p-1.5 text-text-dim hover:text-text-primary transition-colors rounded hover:bg-bg-secondary"
                      title="Search (Ctrl+F)"
                    >
                      <Search size={14} />
                    </button>
                    <button
                      onClick={handleSaveIni}
                      disabled={iniSaving}
                      className="px-3 py-1.5 bg-accent text-on-accent rounded-lg text-xs font-medium hover:bg-accent-hover transition-colors disabled:opacity-40"
                    >
                      {iniSaving ? 'Saving...' : iniSaved ? 'Saved!' : 'Save'}
                    </button>
                  </div>
                </div>
                {iniSearchVisible && (
                  <div className="flex items-center gap-2 mb-2 px-3 py-2 bg-bg-secondary border border-border rounded-lg">
                    <Search size={13} className="text-text-dim flex-shrink-0" />
                    <input
                      ref={iniSearchRef}
                      type="text"
                      value={iniSearch}
                      onChange={e => setIniSearch(e.target.value)}
                      placeholder="Search php.ini..."
                      className="flex-1 bg-transparent text-xs text-text-primary placeholder-text-dim focus:outline-none font-mono"
                    />
                    {iniSearch && (
                      <span className="text-xs text-text-muted flex-shrink-0">{iniMatchCount} match{iniMatchCount !== 1 ? 'es' : ''}</span>
                    )}
                    <button onClick={() => { setIniSearchVisible(false); setIniSearch('') }} className="text-text-dim hover:text-text-primary">
                      <X size={13} />
                    </button>
                  </div>
                )}
                <textarea
                  ref={iniTextareaRef}
                  value={iniContent}
                  onChange={e => setIniContent(e.target.value)}
                  spellCheck={false}
                  className="flex-1 w-full px-4 py-3 bg-bg-secondary border border-border rounded-lg font-mono text-xs text-text-primary leading-relaxed resize-none focus:outline-none focus:border-accent/50 transition-colors"
                />
              </div>
            )}
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center text-text-dim text-sm">
            Select a PHP version to manage extensions
          </div>
        )}
      </div>
    </div>
  )
}
