import { useEffect, useMemo, useState } from 'react'
import { Check, Download, Loader2, AlertCircle, Zap } from 'lucide-react'
import { statusLabel } from '../../lib/i18n'

interface BundleItem {
  name: string
  version: string
  label: string
  required: boolean
}

interface BundleStatus {
  name: string
  version: string
  label: string
  status: 'pending' | 'downloading' | 'extracting' | 'verifying' | 'installed' | 'active' | 'error' | string
  percent: number
  downloaded?: number
  total?: number
  speed_mbps?: number
  eta_seconds?: number
  error?: string
}

function formatBytes(n?: number): string {
  if (!n || n <= 0) return ''
  if (n < 1024 * 1024) return (n / 1024).toFixed(0) + ' KB'
  return (n / 1024 / 1024).toFixed(1) + ' MB'
}

function formatETA(seconds?: number): string {
  if (!seconds || seconds <= 0) return ''
  if (seconds < 60) return seconds + 's'
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return m + 'm ' + s + 's'
}

interface WelcomeWizardProps {
  onComplete: () => void
}

export default function WelcomeWizard({ onComplete }: WelcomeWizardProps) {
  const [bundle, setBundle] = useState<BundleItem[]>([])
  const [selected, setSelected] = useState<Record<string, boolean>>({})
  const [statuses, setStatuses] = useState<Record<string, BundleStatus>>({})
  const [phase, setPhase] = useState<'choose' | 'installing' | 'done'>('choose')
  const [error, setError] = useState<string>('')

  useEffect(() => {
    // @ts-ignore Wails bindings
    if (window.go?.app?.App?.GetDefaultBundle) {
      // @ts-ignore
      window.go.app.App.GetDefaultBundle().then((items: BundleItem[]) => {
        setBundle(items || [])
        const sel: Record<string, boolean> = {}
        for (const it of items || []) sel[key(it)] = it.required || true
        setSelected(sel)
      })
    }
  }, [])

  useEffect(() => {
    if (!window.runtime) return
    const handler = (...args: any[]) => {
      const s = args[0] as BundleStatus
      if (!s) return
      setStatuses(prev => ({ ...prev, [`${s.name}-${s.version}`]: s }))
    }
    window.runtime.EventsOn('bootstrap:progress', handler)
    return () => {
      window.runtime?.EventsOff?.('bootstrap:progress')
    }
  }, [])

  const allTerminal = useMemo(() => {
    if (bundle.length === 0) return false
    const required = bundle.filter(b => selected[key(b)])
    if (required.length === 0) return false
    return required.every(b => {
      const s = statuses[key(b)]
      return s && (s.status === 'active' || s.status === 'installed' || s.status === 'error')
    })
  }, [bundle, selected, statuses])

  useEffect(() => {
    if (phase === 'installing' && allTerminal) {
      setPhase('done')
    }
  }, [phase, allTerminal])

  async function handleInstall() {
    const items = bundle.filter(b => selected[key(b)])
    if (items.length === 0) {
      setError('Wähle mindestens ein Paket zum Installieren.')
      return
    }
    setPhase('installing')
    setError('')
    try {
      // @ts-ignore Wails bindings
      await window.go.app.App.RunBootstrap(items)
    } catch (e) {
      setError(String(e))
    }
  }

  function handleSkip() {
    onComplete()
  }

  function handleFinish() {
    onComplete()
  }

  const totalPct = useMemo(() => {
    const items = bundle.filter(b => selected[key(b)])
    if (items.length === 0) return 0
    let sum = 0
    for (const it of items) {
      const s = statuses[key(it)]
      if (!s) continue
      if (s.status === 'active' || s.status === 'installed') sum += 100
      else sum += s.percent || 0
    }
    return Math.round(sum / items.length)
  }, [bundle, selected, statuses])

  return (
    <div className="fixed inset-0 z-50 bg-bg-primary/95 backdrop-blur-sm flex items-center justify-center">
      <div className="w-full max-w-2xl bg-bg-secondary border border-border rounded-lg shadow-2xl p-8">
        <div className="flex items-center gap-3 mb-2">
          <Zap className="text-accent" size={28} />
          <h1 className="text-2xl font-semibold text-text-primary">Willkommen bei Hangar</h1>
        </div>
        <p className="text-text-muted mb-6">
          Hangar kommt schlank daher. Beim ersten Start laden wir nur die Dienste herunter, die du wirklich willst.
          Wähle deine Grundausstattung - weitere Pakete kannst du später auf der Seite Pakete installieren.
        </p>

        {phase === 'choose' && (
          <>
            <div className="space-y-2 mb-6">
              {bundle.map(item => (
                <label
                  key={key(item)}
                  className="flex items-center gap-3 p-3 rounded border border-border hover:bg-bg-primary cursor-pointer"
                >
                  <input
                    type="checkbox"
                    checked={!!selected[key(item)]}
                    disabled={item.required}
                    onChange={e => setSelected(prev => ({ ...prev, [key(item)]: e.target.checked }))}
                    className="w-4 h-4 accent-accent"
                  />
                  <div className="flex-1">
                    <div className="text-text-primary font-medium">{item.label}</div>
                    <div className="text-xs text-text-muted">
                      {item.required ? 'Empfohlen' : 'Optional'}
                    </div>
                  </div>
                </label>
              ))}
            </div>

            {error && (
              <div className="flex items-center gap-2 text-status-red text-sm mb-4">
                <AlertCircle size={16} /> {error}
              </div>
            )}

            <div className="flex justify-between">
              <button
                onClick={handleSkip}
                className="px-4 py-2 text-text-muted hover:text-text-primary transition-colors"
              >
                Erst mal überspringen
              </button>
              <button
                onClick={handleInstall}
                className="px-6 py-2 bg-accent text-on-accent rounded font-medium hover:bg-accent/90 transition-colors flex items-center gap-2"
              >
                <Download size={16} /> Auswahl installieren
              </button>
            </div>
          </>
        )}

        {(phase === 'installing' || phase === 'done') && (
          <>
            <div className="mb-6">
              <div className="flex justify-between text-xs text-text-muted mb-2">
                <span>Gesamtfortschritt</span>
                <span>{totalPct}%</span>
              </div>
              <div className="h-2 bg-bg-primary rounded overflow-hidden">
                <div
                  className="h-full bg-accent transition-all"
                  style={{ width: `${totalPct}%` }}
                />
              </div>
            </div>

            <div className="space-y-3 max-h-96 overflow-y-auto pr-2">
              {bundle.filter(b => selected[key(b)]).map(item => {
                const s = statuses[key(item)]
                const pct = s?.percent ?? 0
                const status = s?.status ?? 'pending'
                return (
                  <div key={key(item)} className="border border-border rounded p-3">
                    <div className="flex items-center justify-between mb-2">
                      <div className="flex items-center gap-2 text-sm text-text-primary">
                        {status === 'active' || status === 'installed' ? (
                          <Check size={14} className="text-status-green" />
                        ) : status === 'error' ? (
                          <AlertCircle size={14} className="text-status-red" />
                        ) : status === 'pending' ? (
                          <span className="w-3.5 h-3.5 rounded-full border border-text-muted" />
                        ) : (
                          <Loader2 size={14} className="text-status-yellow animate-spin" />
                        )}
                        <span>{item.label}</span>
                      </div>
                      <span className="text-xs text-text-muted">{statusLabel(status)}</span>
                    </div>
                    {(status === 'downloading' || status === 'extracting' || status === 'verifying') && (
                      <>
                        <div className="h-1 bg-bg-primary rounded overflow-hidden">
                          <div
                            className="h-full bg-status-yellow transition-all"
                            style={{ width: `${pct}%` }}
                          />
                        </div>
                        {status === 'downloading' && (s?.speed_mbps || s?.total) && (
                          <div className="flex justify-between text-[11px] text-text-muted mt-1.5 font-mono">
                            <span>
                              {formatBytes(s?.downloaded)}
                              {s?.total ? ' / ' + formatBytes(s.total) : ''}
                            </span>
                            <span>
                              {s?.speed_mbps && s.speed_mbps > 0 ? s.speed_mbps.toFixed(1) + ' MB/s' : ''}
                              {s?.eta_seconds && s.eta_seconds > 0 ? ' · noch ' + formatETA(s.eta_seconds) : ''}
                            </span>
                          </div>
                        )}
                      </>
                    )}
                    {s?.error && (
                      <div className="text-xs text-status-red mt-1 break-words">{s.error}</div>
                    )}
                  </div>
                )
              })}
            </div>

            <div className="mt-6 flex justify-between items-center">
              {/* Always offer an escape hatch. If a download is hanging or
                  failed and the user just wants to get into the app to use
                  what they already installed, this lets them. */}
              <button
                onClick={handleFinish}
                className="px-4 py-2 text-text-muted hover:text-text-primary transition-colors text-sm"
              >
                {phase === 'done' ? 'Rest überspringen' : 'Überspringen und zur App'}
              </button>
              {phase === 'done' && (
                <button
                  onClick={handleFinish}
                  className="px-6 py-2 bg-accent text-on-accent rounded font-medium hover:bg-accent/90 transition-colors"
                >
                  Los geht's
                </button>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function key(b: { name: string; version: string }) {
  return `${b.name}-${b.version}`
}
