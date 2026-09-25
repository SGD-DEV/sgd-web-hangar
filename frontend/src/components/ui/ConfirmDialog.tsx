import { useEffect } from 'react'
import { AlertTriangle, HelpCircle } from 'lucide-react'
import { useConfirm } from '../../lib/api'
import { Button } from './Controls'

// Renders the dialog opened by confirmDialog(). Mounted once in App, above
// every modal so it also works from inside one.
export default function ConfirmDialog() {
  const { current, close } = useConfirm()

  useEffect(() => {
    if (!current) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.stopPropagation(); close(false) }
      if (e.key === 'Enter') { e.preventDefault(); close(true) }
    }
    // Capture phase: the Escape must not also close the modal underneath.
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [current, close])

  if (!current) return null
  const Icon = current.danger ? AlertTriangle : HelpCircle

  return (
    <div className="fixed inset-0 z-[90] bg-black/60 flex items-center justify-center p-6" onMouseDown={() => close(false)}>
      <div
        className="w-full max-w-md bg-bg-sidebar border border-border rounded-xl shadow-2xl"
        onMouseDown={e => e.stopPropagation()}
        role="alertdialog"
        aria-modal="true"
        aria-label={current.title}
      >
        <div className="flex gap-3 p-5">
          <div className={`flex-shrink-0 w-9 h-9 rounded-full flex items-center justify-center ${current.danger ? 'bg-status-red/10 text-status-red' : 'bg-accent/10 text-accent'}`}>
            <Icon size={18} />
          </div>
          <div className="min-w-0 pt-1">
            <h3 className="text-sm font-medium text-text-primary">{current.title}</h3>
            {current.message && (
              <p className="text-xs text-text-muted mt-2 whitespace-pre-wrap break-words leading-relaxed">{current.message}</p>
            )}
          </div>
        </div>
        <div className="flex justify-end gap-2 px-5 py-3 border-t border-border">
          <Button onClick={() => close(false)}>Cancel</Button>
          <Button variant={current.danger ? 'danger' : 'primary'} onClick={() => close(true)}>
            {current.confirmLabel || 'OK'}
          </Button>
        </div>
      </div>
    </div>
  )
}
