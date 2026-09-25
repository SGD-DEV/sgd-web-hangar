import { useState, useEffect, useCallback } from 'react'
import { Shield, RefreshCw, Plus, X, FolderOpen, Loader2 } from 'lucide-react'
import { call, useAction, toast, errorMessage } from '../../lib/api'
import { Button, Card, Field, inputCls } from '../ui/Controls'

interface CertInfo {
  domain: string
  cert_path: string
  key_path: string
  expires_at: string
  created_at: string
}

export default function SSLManager() {
  const [certs, setCerts] = useState<CertInfo[]>([])
  const [caInstalled, setCAInstalled] = useState<boolean | null>(null)
  const [caPath, setCAPath] = useState('')
  const [loading, setLoading] = useState(true)
  const [showGenerate, setShowGenerate] = useState(false)
  const [newDomain, setNewDomain] = useState('')

  const load = useCallback(async () => {
    try {
      setCerts((await call<CertInfo[]>('GetSSLCerts') || []).filter(c => c.domain).sort((a, b) => a.domain.localeCompare(b.domain)))
      setCAInstalled(await call<boolean>('IsRootCAInstalled'))
      setCAPath(await call<string>('GetSSLCAPath'))
    } catch (e) {
      toast.error('Zertifikate konnten nicht geladen werden', errorMessage(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const [installCA, installing] = useAction(async () => {
    await call('InstallRootCA')
    await load()
  }, { success: 'Root-CA installiert - Browser vertrauen jetzt lokalem HTTPS', error: 'Root-CA nicht installiert' })

  const [generate, generating] = useAction(async (domain: string) => {
    await call('GenerateSSLCert', domain.trim().toLowerCase())
    setShowGenerate(false)
    setNewDomain('')
    await load()
  }, { success: 'Zertifikat erstellt', error: 'Zertifikat konnte nicht erstellt werden' })

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-lg font-medium">SSL-Zertifikate</h2>
          <p className="text-xs text-text-muted mt-1">
            Lokales HTTPS für <span className="font-mono">.test</span>-Seiten über mkcert. Öffentliche Domains bekommen HTTPS von Cloudflare (Seite Tunnel) und brauchen hier nichts.
          </p>
        </div>
        <Button onClick={() => setShowGenerate(true)} icon={<Plus size={12} />}>Zertifikat erstellen</Button>
      </div>

      {showGenerate && (
        <Card className="mb-6 p-4 border-accent/20">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-medium">SSL-Zertifikat erstellen</h3>
            <button onClick={() => setShowGenerate(false)} className="text-text-dim hover:text-text-primary"><X size={14} /></button>
          </div>
          <Field label="Domain" hint="Meist nicht nötig: Wenn du HTTPS für ein Projekt einschaltest (Schloss-Symbol), wird sein Zertifikat automatisch erstellt.">
            <div className="flex gap-2">
              <input className={inputCls} value={newDomain} onChange={e => setNewDomain(e.target.value)} placeholder="myapp.test"
                onKeyDown={e => e.key === 'Enter' && newDomain && generate(newDomain)} />
              <Button variant="primary" busy={generating} disabled={!newDomain} onClick={() => generate(newDomain)}>Erstellen</Button>
            </div>
          </Field>
        </Card>
      )}

      <div className={`rounded-lg p-4 border mb-6 ${caInstalled ? 'bg-bg-selected border-accent/20' : 'bg-status-yellow/5 border-status-yellow/20'}`}>
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            {caInstalled === null
              ? <Loader2 size={16} className="animate-spin text-text-dim" />
              : <Shield size={16} className={caInstalled ? 'text-accent' : 'text-status-yellow'} />}
            <div>
              <p className="text-sm font-medium">
                {caInstalled === null ? 'Wird geprüft…' : caInstalled ? 'Root-CA installiert' : 'Root-CA nicht installiert'}
              </p>
              <p className="text-xs text-text-muted mt-0.5">
                {caInstalled
                  ? 'Windows vertraut Hangars lokaler Zertifizierungsstelle - Browser akzeptieren https://*.test ohne Warnung.'
                  : 'Einmal installieren, damit Browser den lokalen Zertifikaten vertrauen. Windows fragt nach einer Bestätigung - klicke auf Ja.'}
              </p>
              {caPath && <p className="text-[11px] text-text-dim font-mono mt-1">{caPath}</p>}
            </div>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            {caPath && (
              <Button size="xs" variant="ghost" icon={<FolderOpen size={11} />} onClick={() => call('OpenInExplorer', caPath).catch(e => toast.error('Öffnen fehlgeschlagen', errorMessage(e)))}>
                Ordner
              </Button>
            )}
            {caInstalled === false && <Button variant="primary" busy={installing} onClick={installCA}>CA installieren</Button>}
          </div>
        </div>
      </div>

      {loading ? (
        <div className="text-text-dim text-sm">Lädt…</div>
      ) : certs.length === 0 ? (
        <Card className="p-8 text-center">
          <Shield size={32} className="mx-auto text-text-dim mb-3" />
          <p className="text-text-muted text-sm">Noch keine Zertifikate</p>
          <p className="text-text-dim text-xs mt-2">Schalte HTTPS für ein Projekt über das Schloss-Symbol auf der Seite Projekte ein.</p>
        </Card>
      ) : (
        <div className="space-y-2">
          {certs.map(c => (
            <Card key={c.domain} className="flex items-center justify-between px-4 py-3">
              <div className="min-w-0">
                <span className="text-sm font-medium font-mono">{c.domain}</span>
                <div className="text-xs text-text-dim mt-0.5 truncate">
                  Gültig bis {new Date(c.expires_at).toLocaleDateString('de-DE')} · <span className="font-mono">{c.cert_path}</span>
                </div>
              </div>
              <Button size="xs" variant="ghost" busy={generating} onClick={() => generate(c.domain)} icon={<RefreshCw size={11} />} title="Ein neues Zertifikat für diese Domain erstellen">
                Erneuern
              </Button>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
