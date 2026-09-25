import { useEffect, useRef, useState } from 'react'
import { ImagePlus, Trash2, RotateCcw } from 'lucide-react'
import { call, useAction } from '../../lib/api'
import { useAppearance, applyAccent, type AppearanceSettings } from '../../lib/theme'
import { Button, Field, inputCls, Card } from '../ui/Controls'

const accentPresets = ['#b5f23d', '#22c55e', '#14b8a6', '#3b82f6', '#6366f1', '#a855f7', '#ec4899', '#f97316', '#eab308', '#ef4444']
const backgroundPresets = ['#0f1010', '#111827', '#1e1b2e', '#ffffff', '#f5f5f4']

function ColorInput({ value, onChange, presets, allowEmpty, emptyLabel }: {
  value: string
  onChange: (v: string) => void
  presets: string[]
  allowEmpty?: boolean
  emptyLabel?: string
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <input type="color" value={value || '#b5f23d'} onChange={e => onChange(e.target.value)}
          className="w-9 h-8 rounded border border-border bg-bg-primary cursor-pointer p-0.5" aria-label="Farbe wählen" />
        <input className={`${inputCls} w-32`} value={value} placeholder={allowEmpty ? emptyLabel : '#b5f23d'}
          onChange={e => onChange(e.target.value.trim())} />
        {allowEmpty && value && (
          <button onClick={() => onChange('')} className="text-[11px] text-text-dim hover:text-text-primary">{emptyLabel}</button>
        )}
      </div>
      <div className="flex flex-wrap gap-1.5">
        {presets.map(c => (
          <button key={c} onClick={() => onChange(c)} title={c}
            className={`w-5 h-5 rounded-full border ${value.toLowerCase() === c ? 'ring-2 ring-offset-2 ring-offset-bg-secondary ring-text-primary border-transparent' : 'border-border'}`}
            style={{ background: c }} />
        ))}
      </div>
    </div>
  )
}

export default function AppearancePage() {
  const { settings: saved, set: setSaved, error: loadError, load } = useAppearance()
  const [form, setForm] = useState<AppearanceSettings | null>(saved)
  const [texts, setTexts] = useState<Record<string, [string, string]>>({})
  const [preview, setPreview] = useState('')
  const previewTimer = useRef<number>()

  useEffect(() => { if (saved && !form) setForm(saved) }, [saved, form])
  // The startup load may have failed; try again when the page opens.
  useEffect(() => { if (!useAppearance.getState().settings) load() }, [load])
  useEffect(() => { call<Record<string, [string, string]>>('GetStarterTexts').then(t => setTexts(t || {})).catch(() => {}) }, [])

  // Live preview: accent applies to Hangar itself at once, the start page
  // is re-rendered by the backend shortly after typing stops.
  useEffect(() => {
    if (!form) return
    applyAccent(form.accent)
    window.clearTimeout(previewTimer.current)
    previewTimer.current = window.setTimeout(() => {
      call<string>('PreviewStarterPage', form).then(setPreview).catch(() => {})
    }, 250)
  }, [form])

  // Leaving the page without saving restores the saved accent.
  useEffect(() => () => { const s = useAppearance.getState().settings; if (s) applyAccent(s.accent) }, [])

  const [save, saving] = useAction(async () => {
    const s = await call<AppearanceSettings>('SaveAppearance', form)
    setSaved(s)
    setForm(s)
  }, { success: 'Erscheinungsbild gespeichert', error: 'Speichern fehlgeschlagen' })

  const [pickLogo, picking] = useAction(async () => {
    const s = await call<AppearanceSettings>('PickAppearanceLogo')
    setSaved({ ...useAppearance.getState().settings!, logo: s.logo })
    setForm(f => f && { ...f, logo: s.logo })
  }, { error: 'Dieses Logo kann nicht verwendet werden' })

  const [removeLogo] = useAction(async () => {
    const s = await call<AppearanceSettings>('RemoveAppearanceLogo')
    setSaved({ ...useAppearance.getState().settings!, logo: s.logo })
    setForm(f => f && { ...f, logo: '' })
  }, { error: 'Logo konnte nicht entfernt werden' })

  if (!form) {
    return (
      <div className="flex-1 p-6 text-sm">
        {loadError
          ? <div className="space-y-3">
              <p className="text-status-red">Erscheinungsbild konnte nicht geladen werden: <span className="font-mono">{loadError}</span></p>
              <Button onClick={() => load()}>Erneut versuchen</Button>
            </div>
          : <span className="text-text-dim">Lädt…</span>}
      </div>
    )
  }

  const set = (patch: Partial<AppearanceSettings>) => setForm(f => f && { ...f, ...patch })
  const setStarter = (patch: Partial<AppearanceSettings['starter']>) => setForm(f => f && { ...f, starter: { ...f.starter, ...patch } })
  const dirty = JSON.stringify({ ...form, logo: '' }) !== JSON.stringify({ ...saved, logo: '' })

  function setLang(lang: string) {
    const t = texts[lang]
    const current = texts[form!.starter.lang]
    // Swap the texts along with the language unless they were customised.
    const untouched = current && form!.starter.heading === current[0] && form!.starter.text === current[1]
    setStarter(t && untouched ? { lang, heading: t[0], text: t[1] } : { lang })
  }

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="flex items-center justify-between mb-6 max-w-6xl">
        <div>
          <h2 className="text-lg font-medium">Erscheinungsbild</h2>
          <p className="text-xs text-text-muted mt-1">Name, Farbe und Logo dieses Panels sowie die Startseite für neue Projekte.</p>
        </div>
        <div className="flex gap-2">
          {dirty && <Button onClick={() => { setForm(saved); if (saved) applyAccent(saved.accent) }} icon={<RotateCcw size={12} />}>Verwerfen</Button>}
          <Button variant="primary" busy={saving} disabled={!dirty} onClick={save}>Speichern</Button>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-6 max-w-6xl">
        <div className="space-y-6">
          <Card className="p-4 space-y-4">
            <h3 className="text-sm font-medium">Panel</h3>
            <Field label="Name" hint="Erscheint oben in der Seitenleiste und als Fenstertitel.">
              <input className={inputCls} value={form.app_name} maxLength={40} onChange={e => set({ app_name: e.target.value })} />
            </Field>
            <div>
              <span className="block text-xs text-text-muted mb-1">Logo</span>
              <div className="flex items-center gap-3">
                <div className="w-14 h-14 rounded-lg border border-border bg-bg-primary flex items-center justify-center overflow-hidden">
                  {form.logo ? <img src={form.logo} alt="" className="max-w-full max-h-full object-contain" /> : <span className="text-[10px] text-text-dim">keins</span>}
                </div>
                <Button busy={picking} onClick={pickLogo} icon={<ImagePlus size={12} />}>{form.logo ? 'Ersetzen…' : 'Auswählen…'}</Button>
                {form.logo && <Button variant="ghost" onClick={removeLogo} icon={<Trash2 size={12} />}>Entfernen</Button>}
              </div>
              <span className="block text-[11px] text-text-dim mt-1">PNG, JPG, SVG oder WebP, bis 512 KB. Wirkt sofort.</span>
            </div>
            <Field label="Akzentfarbe" hint="Buttons, Hervorhebungen und der aktive Menüpunkt. Die Schrift darauf wechselt automatisch zwischen dunkel und hell.">
              <ColorInput value={form.accent} onChange={accent => set({ accent })} presets={accentPresets} />
            </Field>
          </Card>

          <Card className="p-4 space-y-4">
            <h3 className="text-sm font-medium">Startseite für neue Projekte</h3>
            <p className="text-[11px] text-text-dim -mt-2">
              Neue PHP-Projekte bekommen diese index.php. Platzhalter: <span className="font-mono">{'{name} {domain} {php} {folder}'}</span>. Bestehende Projekte behalten ihre Seite.
            </p>
            <Field label="Sprache">
              <select className={inputCls} value={form.starter.lang} onChange={e => setLang(e.target.value)}>
                <option value="de">Deutsch</option>
                <option value="en">English</option>
              </select>
            </Field>
            <Field label="Überschrift">
              <input className={inputCls} value={form.starter.heading} onChange={e => setStarter({ heading: e.target.value })} />
            </Field>
            <Field label="Text">
              <textarea className={`${inputCls} h-20 resize-y`} value={form.starter.text} onChange={e => setStarter({ text: e.target.value })} />
            </Field>
            <div className="grid grid-cols-2 gap-4">
              <Field label="Hintergrund">
                <ColorInput value={form.starter.background} onChange={background => setStarter({ background })} presets={backgroundPresets} />
              </Field>
              <Field label="Akzent">
                <ColorInput value={form.starter.accent} onChange={accent => setStarter({ accent })} presets={accentPresets.slice(0, 5)} allowEmpty emptyLabel="wie Panel" />
              </Field>
            </div>
            <div className="space-y-2">
              <label className="flex items-center gap-2 text-sm text-text-muted cursor-pointer">
                <input type="checkbox" className="accent-accent" checked={form.starter.show_logo} onChange={e => setStarter({ show_logo: e.target.checked })} />
                Logo anzeigen
              </label>
              <label className="flex items-center gap-2 text-sm text-text-muted cursor-pointer">
                <input type="checkbox" className="accent-accent" checked={form.starter.show_php} onChange={e => setStarter({ show_php: e.target.checked })} />
                PHP-Version nennen (der Satz mit {'{php}'})
              </label>
            </div>
          </Card>
        </div>

        <div className="xl:sticky xl:top-0 self-start">
          <span className="block text-xs text-text-muted mb-1">Vorschau</span>
          <div className="rounded-lg border border-border overflow-hidden bg-bg-primary">
            <div className="flex items-center gap-1.5 px-3 py-2 border-b border-border bg-bg-secondary">
              <span className="w-2.5 h-2.5 rounded-full bg-status-red/60" />
              <span className="w-2.5 h-2.5 rounded-full bg-status-yellow/60" />
              <span className="w-2.5 h-2.5 rounded-full bg-status-green/60" />
              <span className="ml-3 text-[11px] font-mono text-text-dim">http://mein-projekt.test</span>
            </div>
            <iframe title="Vorschau der Startseite" srcDoc={preview} sandbox="" className="w-full h-[420px] bg-white" />
          </div>
        </div>
      </div>
    </div>
  )
}
