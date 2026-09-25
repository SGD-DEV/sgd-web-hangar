import { useEffect, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { call, toast, errorMessage } from '../../lib/api'

interface PoolStatus {
  version: string
  port: number
  workers: number
  alive: number
  restarts: number
}

// WebServerSwitch picks which web server owns port 80. Both serve the same
// projects, so switching keeps every URL working.
export default function WebServerSwitch({ statuses }: { statuses: Record<string, { status: string } | undefined> }) {
  const [active, setActive] = useState('apache')
  const [busy, setBusy] = useState<string | null>(null)
  const [pool, setPool] = useState<PoolStatus[]>([])

  useEffect(() => {
    call<string>('GetActiveWebServer').then(setActive).catch(() => {})
  }, [])

  useEffect(() => {
    call<PoolStatus[]>('GetPHPPoolStatus').then(p => setPool(p || [])).catch(() => {})
  }, [statuses])

  async function switchTo(target: string) {
    if (busy) return
    setBusy(target)
    try {
      await call('SwitchWebServer', target)
      setActive(target)
      toast.success(`${target === 'apache' ? 'Apache' : 'Nginx'} liefert jetzt alle Seiten aus`)
    } catch (e) {
      toast.error(`Wechsel zu ${target} fehlgeschlagen`, errorMessage(e))
    } finally {
      setBusy(null)
    }
  }

  const running = (n: string) => statuses[n]?.status === 'running'

  return (
    <div className="mb-5">
      <h2 className="text-xs font-medium text-text-muted uppercase tracking-wider mb-2">Webserver</h2>
      <div className="grid grid-cols-2 gap-1 p-1 bg-bg-secondary border border-border rounded-lg">
        {['apache', 'nginx'].map(name => {
          const isActive = active === name
          return (
            <button
              key={name}
              onClick={() => switchTo(name)}
              disabled={!!busy}
              className={`flex items-center justify-center gap-2 py-1.5 rounded-md text-xs font-medium transition-colors
                ${isActive ? 'bg-accent/15 text-accent' : 'text-text-muted hover:text-text-primary'} disabled:cursor-wait`}
              title={isActive ? `${name} ist der aktive Webserver` : `Den anderen Server stoppen und alle Seiten mit ${name} ausliefern`}
            >
              {busy === name
                ? <Loader2 size={11} className="animate-spin" />
                : <span className={`w-1.5 h-1.5 rounded-full ${running(name) ? 'bg-status-green' : 'bg-text-dim'}`} />}
              {name === 'apache' ? 'Apache' : 'Nginx'}
            </button>
          )
        })}
      </div>
      {pool.length > 0 && (
        <p className="text-[11px] text-text-dim mt-2">
          PHP-Worker: {pool.map(p => `${p.version} (${p.alive}/${p.workers})`).join(', ')}
        </p>
      )}
    </div>
  )
}
