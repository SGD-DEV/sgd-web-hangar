import { useState, useEffect, useRef, useCallback } from 'react'
import { Terminal as TerminalIcon, ExternalLink } from 'lucide-react'

interface OutputLine {
  type: 'command' | 'stdout' | 'stderr' | 'info'
  text: string
}

export default function DevourTerminal() {
  const [input, setInput] = useState('')
  const [lines, setLines] = useState<OutputLine[]>([])
  const [running, setRunning] = useState(false)
  const [envInfo, setEnvInfo] = useState<Record<string, string>>({})
  const [cmdHistory, setCmdHistory] = useState<string[]>([])
  const [historyIdx, setHistoryIdx] = useState(-1)
  const scrollRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const runningRef = useRef(false)

  useEffect(() => {
    loadEnvInfo()
  }, [])

  useEffect(() => {
    scrollRef.current?.scrollTo(0, scrollRef.current.scrollHeight)
  }, [lines])

  // Listen for streaming terminal events
  useEffect(() => {
    const w = window as any
    if (!w.runtime?.EventsOn) return

    const offOutput = w.runtime.EventsOn('terminal:output', (data: any) => {
      if (data?.data) {
        const type = data.type === 'stderr' ? 'stderr' : 'stdout'
        setLines(prev => [...prev, { type, text: data.data }])
      }
    })

    const offDone = w.runtime.EventsOn('terminal:done', () => {
      setRunning(false)
      runningRef.current = false
      setTimeout(() => inputRef.current?.focus(), 50)
    })

    return () => {
      if (typeof offOutput === 'function') offOutput()
      if (typeof offDone === 'function') offDone()
    }
  }, [])

  async function loadEnvInfo() {
    try {
      const info = await window.go?.app?.App?.GetTerminalEnvInfo?.()
      setEnvInfo(info || {})
    } catch (e) {
      console.error(e)
    }
  }

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault()
    if (!input.trim()) return

    const cmd = input.trim()
    setInput('')

    // If a command is running, send as stdin input
    if (runningRef.current) {
      setLines(prev => [...prev, { type: 'info', text: cmd }])
      try {
        await window.go?.app?.App?.SendTerminalInput?.(cmd)
      } catch (err: any) {
        setLines(prev => [...prev, { type: 'stderr', text: 'Failed to send input: ' + (err?.message || String(err)) }])
      }
      return
    }

    setCmdHistory(prev => [cmd, ...prev])
    setHistoryIdx(-1)

    // Handle built-in commands
    if (cmd === 'clear' || cmd === 'cls') {
      setLines([])
      return
    }

    setLines(prev => [...prev, { type: 'command', text: cmd }])
    setRunning(true)
    runningRef.current = true

    try {
      const result = await window.go?.app?.App?.RunTerminalCommand?.(cmd)
      // Final output may arrive via events, but also check result for any remaining output
      if (result?.output && result.output.trim()) {
        // Output already streamed via events; only add if events didn't fire
      }
      if (result?.error && result.error.trim()) {
        setLines(prev => [...prev, { type: 'stderr', text: result.error }])
      }
    } catch (err: any) {
      setLines(prev => [...prev, { type: 'stderr', text: err?.message || String(err) }])
    } finally {
      setRunning(false)
      runningRef.current = false
      setTimeout(() => inputRef.current?.focus(), 50)
    }
  }, [input])

  function handleKeyDown(e: React.KeyboardEvent) {
    // Ctrl+C to cancel running command
    if (e.key === 'c' && e.ctrlKey && runningRef.current) {
      e.preventDefault()
      window.go?.app?.App?.CancelTerminalCommand?.()
      setLines(prev => [...prev, { type: 'info', text: '^C' }])
      return
    }

    if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (cmdHistory.length > 0 && !runningRef.current) {
        const newIdx = Math.min(historyIdx + 1, cmdHistory.length - 1)
        setHistoryIdx(newIdx)
        setInput(cmdHistory[newIdx])
      }
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (historyIdx > 0 && !runningRef.current) {
        const newIdx = historyIdx - 1
        setHistoryIdx(newIdx)
        setInput(cmdHistory[newIdx])
      } else {
        setHistoryIdx(-1)
        setInput('')
      }
    }
  }

  const prompt = envInfo.cwd ? envInfo.cwd.replace(/\\/g, '/').split('/').pop() : 'projects'

  return (
    <div className="flex-1 flex flex-col overflow-hidden bg-bg-primary">
      {/* Header */}
      <div className="px-6 py-3 border-b border-border flex items-center justify-between">
        <div className="flex items-center gap-2">
          <TerminalIcon size={14} className="text-accent" />
          <h2 className="text-sm font-medium">Hangar Terminal</h2>
          {envInfo.php && (
            <span className="text-xs text-text-muted bg-bg-secondary px-2 py-0.5 rounded font-mono">PHP {envInfo.php}</span>
          )}
        </div>
        <div className="flex items-center gap-3">
          <span className="text-xs text-text-dim font-mono">{envInfo.cwd || ''}</span>
          <button
            onClick={async () => {
              try {
                // @ts-ignore Wails bindings
                await window.go?.app?.App?.OpenExternalTerminal?.(envInfo.cwd || '')
              } catch (e: any) {
                console.error('open external terminal:', e)
              }
            }}
            className="flex items-center gap-1.5 px-2.5 py-1 bg-bg-secondary border border-border rounded text-xs text-text-muted hover:text-text-primary hover:border-text-dim transition-colors"
            title="Opens Windows Terminal (or cmd.exe) with PHP/MySQL/psql/composer on PATH. Use this for interactive REPLs like psql or mysql shell."
          >
            <ExternalLink size={11} />
            Open in Windows Terminal
          </button>
        </div>
      </div>

      {/* Output */}
      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto px-6 py-4 font-mono text-xs leading-relaxed"
        onClick={() => inputRef.current?.focus()}
      >
        {/* Welcome message */}
        {lines.length === 0 && !running && (
          <div className="text-text-dim mb-4">
            <p className="text-accent mb-1">Hangar Terminal v1.0</p>
            <p>PHP, MySQL, Composer, Node and other tools are automatically available.</p>
            <p>Type <span className="text-text-primary">php -v</span>, <span className="text-text-primary">mysql --version</span>, or any command.</p>
            <p>Type <span className="text-text-primary">clear</span> to reset. Press <span className="text-text-primary">Ctrl+C</span> to cancel.</p>
            <p className="mt-2 text-text-muted">
              Interactive REPLs (<span className="text-text-primary">psql</span>, <span className="text-text-primary">mysql</span> shell, <span className="text-text-primary">php artisan tinker</span>, <span className="text-text-primary">npx create-react-app</span>):
              click <span className="text-text-primary">Open in Windows Terminal</span> at the top right.
              This in-app terminal is for non-interactive commands only - REPLs hang
              forever waiting for a TTY we can't give them.
            </p>
          </div>
        )}

        {lines.map((line, i) => {
          switch (line.type) {
            case 'command':
              return (
                <div key={i} className="flex items-center gap-1.5 mt-2">
                  <span className="text-accent">{prompt}</span>
                  <span className="text-text-muted">$</span>
                  <span className="text-text-primary">{line.text}</span>
                </div>
              )
            case 'stdout':
              return <pre key={i} className="text-text-secondary whitespace-pre-wrap">{line.text}</pre>
            case 'stderr':
              return <pre key={i} className="text-status-red whitespace-pre-wrap">{line.text}</pre>
            case 'info':
              return <pre key={i} className="text-text-dim whitespace-pre-wrap">{line.text}</pre>
            default:
              return null
          }
        })}

        {running && (
          <div className="text-text-dim animate-pulse mt-1">Running...</div>
        )}
      </div>

      {/* Input — always enabled so user can type stdin while command runs */}
      <form onSubmit={handleSubmit} className="border-t border-border px-6 py-3 flex items-center gap-2">
        <span className="text-accent font-mono text-xs">{prompt}</span>
        <span className="text-text-muted font-mono text-xs">$</span>
        <input
          ref={inputRef}
          type="text"
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          autoFocus
          spellCheck={false}
          className="flex-1 bg-transparent font-mono text-xs text-text-primary placeholder-text-dim focus:outline-none"
          placeholder={running ? "Type input or Ctrl+C to cancel..." : "Enter command..."}
        />
      </form>
    </div>
  )
}
