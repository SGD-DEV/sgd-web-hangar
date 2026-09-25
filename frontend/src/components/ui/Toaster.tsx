import { CheckCircle2, AlertCircle, Info, X } from 'lucide-react'
import { useToasts } from '../../lib/api'

const styles = {
  success: { icon: CheckCircle2, cls: 'border-status-green/30 text-status-green' },
  error: { icon: AlertCircle, cls: 'border-status-red/40 text-status-red' },
  info: { icon: Info, cls: 'border-accent/30 text-accent' },
}

export default function Toaster() {
  const { toasts, dismiss } = useToasts()
  return (
    <div className="fixed bottom-10 right-4 z-[100] flex flex-col gap-2 w-96 pointer-events-none">
      {toasts.map(t => {
        const { icon: Icon, cls } = styles[t.kind]
        return (
          <div
            key={t.id}
            className={`pointer-events-auto flex items-start gap-3 px-4 py-3 bg-bg-secondary border rounded-lg shadow-xl shadow-black/40 ${cls}`}
            role={t.kind === 'error' ? 'alert' : 'status'}
          >
            <Icon size={16} className="flex-shrink-0 mt-0.5" />
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-text-primary">{t.title}</p>
              {t.detail && (
                <p className="text-xs text-text-muted mt-1 whitespace-pre-wrap break-words font-mono select-text">{t.detail}</p>
              )}
            </div>
            <button onClick={() => dismiss(t.id)} className="text-text-dim hover:text-text-primary" aria-label="Ausblenden">
              <X size={14} />
            </button>
          </div>
        )
      })}
    </div>
  )
}
