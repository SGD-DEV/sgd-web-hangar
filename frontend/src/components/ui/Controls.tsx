import { ReactNode, useEffect } from 'react'
import { Loader2, X } from 'lucide-react'

// Shared building blocks so new pages look like the existing ones and every
// button shows the same busy feedback.

type Variant = 'primary' | 'secondary' | 'danger' | 'ghost'

const variants: Record<Variant, string> = {
  primary: 'bg-accent text-bg-primary hover:bg-accent-hover font-medium',
  secondary: 'bg-bg-secondary border border-border text-text-muted hover:text-text-primary hover:border-text-dim',
  danger: 'bg-status-red/10 text-status-red border border-status-red/20 hover:bg-status-red/20',
  ghost: 'text-text-dim hover:text-text-primary',
}

export function Button({
  children, onClick, busy, disabled, variant = 'secondary', icon, title, size = 'sm', type = 'button',
}: {
  children?: ReactNode
  onClick?: () => void
  busy?: boolean
  disabled?: boolean
  variant?: Variant
  icon?: ReactNode
  title?: string
  size?: 'xs' | 'sm'
  type?: 'button' | 'submit'
}) {
  const pad = size === 'xs' ? 'px-2 py-1 text-xs' : 'px-3 py-1.5 text-xs'
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled || busy}
      title={title}
      aria-busy={busy}
      className={`inline-flex items-center gap-1.5 rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${pad} ${variants[variant]}`}
    >
      {busy ? <Loader2 size={12} className="animate-spin" /> : icon}
      {children}
    </button>
  )
}

export function Modal({ title, onClose, children, width = 'max-w-xl' }: {
  title: string
  onClose: () => void
  children: ReactNode
  width?: string
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return (
    <div className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-6" onMouseDown={onClose}>
      <div
        className={`w-full ${width} max-h-full overflow-y-auto bg-bg-sidebar border border-border rounded-xl shadow-2xl`}
        onMouseDown={e => e.stopPropagation()}
        role="dialog"
        aria-label={title}
      >
        <div className="flex items-center justify-between px-5 py-3 border-b border-border sticky top-0 bg-bg-sidebar">
          <h3 className="text-sm font-medium">{title}</h3>
          <button onClick={onClose} className="text-text-dim hover:text-text-primary" aria-label="Close">
            <X size={14} />
          </button>
        </div>
        <div className="p-5">{children}</div>
      </div>
    </div>
  )
}

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block">
      <span className="block text-xs text-text-muted mb-1">{label}</span>
      {children}
      {hint && <span className="block text-[11px] text-text-dim mt-1">{hint}</span>}
    </label>
  )
}

export const inputCls =
  'w-full px-3 py-1.5 bg-bg-primary border border-border rounded-lg text-sm text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50 font-mono select-text'

export function StatusDot({ state }: { state: 'ok' | 'warn' | 'off' | 'busy' }) {
  const cls = {
    ok: 'bg-status-green',
    warn: 'bg-status-yellow',
    off: 'bg-status-red',
    busy: 'bg-status-yellow animate-pulse',
  }[state]
  return <span className={`inline-block w-2 h-2 rounded-full ${cls}`} />
}

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <div className={`bg-bg-secondary rounded-lg border border-border ${className}`}>{children}</div>
}
