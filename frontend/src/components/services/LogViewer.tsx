import { useState, useEffect, useRef } from 'react'
import { Trash2 } from 'lucide-react'

interface LogViewerProps {
  serviceName: string
}

export default function LogViewer({ serviceName }: LogViewerProps) {
  const [logs, setLogs] = useState<string[]>([])
  const [autoScroll, setAutoScroll] = useState(true)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    loadLogs()
    const interval = setInterval(loadLogs, 2000)
    return () => clearInterval(interval)
  }, [serviceName])

  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
    }
  }, [logs, autoScroll])

  async function loadLogs() {
    try {
      // @ts-ignore
      if (window.go?.app?.App?.GetServiceLogs) {
        // @ts-ignore
        const l = await window.go.app.App.GetServiceLogs(serviceName, 200)
        setLogs(l || [])
      }
    } catch (e) {
      // Not connected
    }
  }

  function handleClear() {
    setLogs([])
  }

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-3">
          <h3 className="text-sm font-medium">Logs — {serviceName}</h3>
          <label className="flex items-center gap-1.5 text-xs text-text-muted cursor-pointer">
            <input
              type="checkbox"
              checked={autoScroll}
              onChange={(e) => setAutoScroll(e.target.checked)}
              className="rounded border-border bg-bg-secondary"
            />
            Auto-scroll
          </label>
        </div>
        <button
          onClick={handleClear}
          className="flex items-center gap-1.5 px-2 py-1 text-xs text-text-dim hover:text-text-muted transition-colors"
        >
          <Trash2 size={10} />
          Clear
        </button>
      </div>

      <div
        ref={containerRef}
        className="flex-1 bg-bg-primary rounded-lg border border-border p-3 overflow-y-auto font-mono text-xs leading-5"
      >
        {logs.length === 0 ? (
          <span className="text-text-dim">No logs available</span>
        ) : (
          logs.map((line, i) => (
            <div key={i} className="text-text-muted hover:text-text-primary transition-colors">
              <span className="text-text-dim select-none mr-3">{String(i + 1).padStart(4, ' ')}</span>
              {line}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
