import { useState, useEffect } from 'react'
import { Save, FileText, ChevronDown } from 'lucide-react'

interface ConfigFile {
  name: string
  path: string
}

interface ConfigEditorProps {
  serviceName: string
}

export default function ConfigEditor({ serviceName }: ConfigEditorProps) {
  const [files, setFiles] = useState<ConfigFile[]>([])
  const [selectedFile, setSelectedFile] = useState<string>('')
  const [content, setContent] = useState<string>('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')
  const [showDropdown, setShowDropdown] = useState(false)

  useEffect(() => {
    loadFileList()
  }, [serviceName])

  useEffect(() => {
    if (selectedFile) {
      loadFile(selectedFile)
    }
  }, [selectedFile])

  async function loadFileList() {
    setError('')
    setContent('')
    setSelectedFile('')
    try {
      const list = await window.go?.app?.App?.GetConfigFileList?.(serviceName)
      setFiles(list || [])
      if (list && list.length > 0) {
        setSelectedFile(list[0].path)
      }
    } catch (e: any) {
      setError(e?.message || String(e))
    }
  }

  async function loadFile(path: string) {
    setLoading(true)
    setError('')
    try {
      const data = await window.go?.app?.App?.ReadConfigFile?.(serviceName, path)
      setContent(data || '')
    } catch (e: any) {
      setContent('')
      setError(e?.message || String(e))
    } finally {
      setLoading(false)
    }
  }

  async function handleSave() {
    if (!selectedFile) return
    setSaving(true)
    setSaved(false)
    setError('')
    try {
      await window.go?.app?.App?.WriteConfigFile?.(serviceName, selectedFile, content)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch (e: any) {
      setError(e?.message || String(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-medium">Configuration</h2>
          <p className="text-xs text-text-muted mt-0.5">Edit {serviceName} config files</p>
        </div>
        <div className="flex items-center gap-2">
          {/* File selector dropdown */}
          <div className="relative">
            <button
              onClick={() => setShowDropdown(!showDropdown)}
              className="flex items-center gap-2 px-3 py-1.5 bg-bg-secondary border border-border rounded-lg text-xs font-mono text-text-muted hover:text-text-primary hover:border-text-dim transition-colors"
            >
              <FileText size={12} />
              {selectedFile || 'Select file'}
              <ChevronDown size={10} />
            </button>
            {showDropdown && files.length > 0 && (
              <div className="absolute right-0 top-full mt-1 w-64 bg-bg-secondary border border-border rounded-lg shadow-lg z-10 py-1 max-h-60 overflow-y-auto">
                {files.map(f => (
                  <button
                    key={f.path}
                    onClick={() => { setSelectedFile(f.path); setShowDropdown(false) }}
                    className={`w-full text-left px-3 py-1.5 text-xs font-mono hover:bg-bg-primary transition-colors
                      ${selectedFile === f.path ? 'text-accent' : 'text-text-muted'}`}
                  >
                    {f.name}
                  </button>
                ))}
              </div>
            )}
          </div>

          <button
            onClick={handleSave}
            disabled={saving || !selectedFile || !content}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-accent text-bg-primary rounded-lg text-xs font-medium hover:bg-accent-hover transition-colors disabled:opacity-40"
          >
            <Save size={12} />
            {saving ? 'Saving...' : saved ? 'Saved!' : 'Save'}
          </button>
        </div>
      </div>

      {error && (
        <div className="text-xs text-status-red bg-status-red/10 border border-status-red/20 rounded-lg px-3 py-2">
          {error}
        </div>
      )}

      {loading ? (
        <div className="text-text-dim text-sm py-8 text-center">Loading...</div>
      ) : !selectedFile ? (
        <div className="text-text-dim text-sm py-8 text-center">
          No config files found. Start the service first to generate configuration.
        </div>
      ) : (
        <textarea
          value={content}
          onChange={e => setContent(e.target.value)}
          spellCheck={false}
          className="w-full h-[calc(100vh-280px)] min-h-[400px] px-4 py-3 bg-bg-secondary border border-border rounded-lg font-mono text-xs text-text-primary leading-relaxed resize-none focus:outline-none focus:border-accent/50 transition-colors"
        />
      )}
    </div>
  )
}
