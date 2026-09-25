import { create } from 'zustand'
import { call } from './api'

// Runtime theming from the Appearance settings: the accent colour is set as
// RGB triplets on :root (see tailwind.config.ts / styles/theme.css).

export interface StarterSettings {
  lang: string
  heading: string
  text: string
  background: string
  accent: string
  show_logo: boolean
  show_php: boolean
}

export interface AppearanceSettings {
  app_name: string
  accent: string
  logo: string
  starter: StarterSettings
}

function rgb(hex: string): [number, number, number] {
  const n = parseInt(hex.replace('#', ''), 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

function mix(a: [number, number, number], b: [number, number, number], t: number) {
  return a.map((v, i) => Math.round(v + (b[i] - v) * t)) as [number, number, number]
}

const triplet = (c: number[]) => c.join(' ')

// Relative luminance (WCAG) to pick readable text on the accent.
function luminance([r, g, b]: [number, number, number]) {
  const f = (v: number) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4 }
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
}

export function applyAccent(hex: string) {
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return
  const c = rgb(hex)
  const root = document.documentElement.style
  root.setProperty('--accent', triplet(c))
  root.setProperty('--accent-hover', triplet(mix(c, [255, 255, 255], 0.18)))
  root.setProperty('--bg-selected', triplet(mix([17, 18, 17], c, 0.12)))
  root.setProperty('--on-accent', luminance(c) > 0.35 ? '15 16 16' : '255 255 255')
}

interface AppearanceState {
  settings: AppearanceSettings | null
  set: (s: AppearanceSettings) => void
  load: () => Promise<void>
}

export const useAppearance = create<AppearanceState>((set) => ({
  settings: null,
  set: (s) => {
    applyAccent(s.accent)
    document.title = s.app_name || 'Hangar'
    set({ settings: s })
  },
  load: async () => {
    try {
      const s = await call<AppearanceSettings>('GetAppearance')
      if (s) useAppearance.getState().set(s)
    } catch { /* keep the built-in look */ }
  },
}))
