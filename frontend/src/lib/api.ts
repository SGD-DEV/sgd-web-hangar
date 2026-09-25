import { useCallback, useRef, useState } from 'react'
import { create } from 'zustand'

// call invokes a Go method bound on App and turns Wails' rejection values
// (plain strings, Error objects) into Errors with a readable message.
export async function call<T = any>(method: string, ...args: any[]): Promise<T> {
  const fn = window.go?.app?.App?.[method]
  if (!fn) throw new Error(`Backend method ${method} is not available`)
  try {
    return await fn(...args)
  } catch (e: any) {
    throw new Error(errorMessage(e))
  }
}

export function errorMessage(e: any): string {
  if (!e) return 'Unknown error'
  if (typeof e === 'string') return e
  return e.message || String(e)
}

// --- Toasts -----------------------------------------------------------------

export type ToastKind = 'success' | 'error' | 'info'

export interface Toast {
  id: number
  kind: ToastKind
  title: string
  detail?: string
}

interface ToastState {
  toasts: Toast[]
  push: (t: Omit<Toast, 'id'>) => void
  dismiss: (id: number) => void
}

let nextId = 1

export const useToasts = create<ToastState>((set, get) => ({
  toasts: [],
  push: (t) => {
    const id = nextId++
    set({ toasts: [...get().toasts, { ...t, id }].slice(-5) })
    // Errors stay longer: they usually carry something worth reading.
    setTimeout(() => get().dismiss(id), t.kind === 'error' ? 9000 : 3500)
  },
  dismiss: (id) => set({ toasts: get().toasts.filter(t => t.id !== id) }),
}))

export const toast = {
  success: (title: string, detail?: string) => useToasts.getState().push({ kind: 'success', title, detail }),
  error: (title: string, detail?: string) => useToasts.getState().push({ kind: 'error', title, detail }),
  info: (title: string, detail?: string) => useToasts.getState().push({ kind: 'info', title, detail }),
}

// useAction wraps an async handler so every button gets the same feedback:
// busy while running (to show a spinner and block double clicks), a toast
// on success (if a message is given) and a toast with the backend's error
// message on failure.
export function useAction<A extends any[], R>(
  fn: (...args: A) => Promise<R>,
  opts: { success?: string | ((r: R) => string); error?: string } = {},
): [(...args: A) => Promise<R | undefined>, boolean] {
  const [busy, setBusy] = useState(false)
  const running = useRef(false)
  const optsRef = useRef(opts)
  optsRef.current = opts
  const fnRef = useRef(fn)
  fnRef.current = fn

  const run = useCallback(async (...args: A) => {
    if (running.current) return undefined
    running.current = true
    setBusy(true)
    try {
      const r = await fnRef.current(...args)
      const s = optsRef.current.success
      if (s) toast.success(typeof s === 'function' ? s(r) : s)
      return r
    } catch (e) {
      toast.error(optsRef.current.error || 'Action failed', errorMessage(e))
      return undefined
    } finally {
      running.current = false
      setBusy(false)
    }
  }, [])

  return [run, busy]
}

// --- Confirm dialog -----------------------------------------------------------

export interface ConfirmOptions {
  title: string
  message?: string
  confirmLabel?: string
  danger?: boolean
}

interface ConfirmState {
  current: (ConfirmOptions & { resolve: (ok: boolean) => void }) | null
  open: (o: ConfirmOptions) => Promise<boolean>
  close: (ok: boolean) => void
}

export const useConfirm = create<ConfirmState>((set, get) => ({
  current: null,
  open: (o) => new Promise<boolean>(resolve => {
    get().current?.resolve(false)
    set({ current: { ...o, resolve } })
  }),
  close: (ok) => {
    get().current?.resolve(ok)
    set({ current: null })
  },
}))

// confirmDialog asks in Hangar's own dialog instead of the browser's
// window.confirm and resolves to true when the user confirms.
export const confirmDialog = (o: ConfirmOptions) => useConfirm.getState().open(o)
