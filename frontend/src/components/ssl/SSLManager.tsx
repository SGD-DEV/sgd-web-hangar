import { useState, useEffect } from 'react'
import { Shield, RefreshCw, Trash2, Plus, X } from 'lucide-react'

interface CertInfo {
  domain: string
  cert_path: string
  key_path: string
  expires_at: string
  created_at: string
}

export default function SSLManager() {
  const [certs, setCerts] = useState<CertInfo[]>([])
  const [caInstalled, setCAInstalled] = useState(false)
  const [loading, setLoading] = useState(true)
  const [showGenerate, setShowGenerate] = useState(false)
  const [newDomain, setNewDomain] = useState('')
  const [genError, setGenError] = useState('')

  useEffect(() => {
    loadCerts()
  }, [])

  async function loadCerts() {
    try {
      if (window.go?.app?.App?.GetSSLCerts) {
        const c = await window.go.app.App.GetSSLCerts()
        setCerts(c || [])
      }
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }

  async function handleInstallCA() {
    try {
      await window.go?.app?.App?.InstallRootCA?.()
      setCAInstalled(true)
    } catch (e) {
      console.error(e)
    }
  }

  async function handleGenerate() {
    if (!newDomain) return
    setGenError('')
    try {
      await window.go.app.App.GenerateSSLCert(newDomain)
      setShowGenerate(false)
      setNewDomain('')
      await loadCerts()
    } catch (e: any) {
      setGenError(e?.message || String(e))
    }
  }

  async function handleRegenerate(domain: string) {
    try {
      await window.go.app.App.GenerateSSLCert(domain)
      await loadCerts()
    } catch (e) {
      console.error(e)
    }
  }

  return (
    <div className="flex-1 p-6 overflow-y-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-lg font-medium">SSL Certificates</h2>
          <p className="text-xs text-text-muted mt-1">Local HTTPS via mkcert</p>
        </div>
        <button
          onClick={() => setShowGenerate(true)}
          className="flex items-center gap-2 px-3 py-1.5 bg-bg-secondary border border-border rounded-lg text-xs text-text-muted hover:text-text-primary hover:border-text-dim transition-colors"
        >
          <Plus size={12} />
          Generate Cert
        </button>
      </div>

      {/* Generate Cert Dialog */}
      {showGenerate && (
        <div className="mb-6 bg-bg-secondary rounded-lg p-4 border border-accent/20">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-medium">Generate SSL Certificate</h3>
            <button onClick={() => setShowGenerate(false)} className="text-text-dim hover:text-text-primary">
              <X size={14} />
            </button>
          </div>
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={newDomain}
              onChange={e => setNewDomain(e.target.value)}
              placeholder="myapp.test"
              className="flex-1 px-3 py-1.5 bg-bg-primary border border-border rounded-lg text-sm text-text-primary placeholder-text-dim focus:outline-none focus:border-accent/50 font-mono"
              onKeyDown={e => e.key === 'Enter' && handleGenerate()}
            />
            <button
              onClick={handleGenerate}
              className="px-4 py-1.5 bg-accent text-bg-primary rounded-lg text-xs font-medium hover:bg-accent-hover transition-colors"
            >
              Generate
            </button>
          </div>
          {genError && <div className="text-xs text-status-red mt-2">{genError}</div>}
        </div>
      )}

      <div className={`rounded-lg p-4 border mb-6 ${caInstalled ? 'bg-bg-selected border-accent/20' : 'bg-status-yellow/5 border-status-yellow/20'}`}>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Shield size={16} className={caInstalled ? 'text-accent' : 'text-status-yellow'} />
            <div>
              <p className="text-sm font-medium">
                {caInstalled ? 'Root CA Installed' : 'Root CA Not Installed'}
              </p>
              <p className="text-xs text-text-muted mt-0.5">
                {caInstalled
                  ? 'mkcert CA is trusted by this system'
                  : 'Install the CA to enable local HTTPS'}
              </p>
            </div>
          </div>
          {!caInstalled && (
            <button
              onClick={handleInstallCA}
              className="px-3 py-1.5 bg-accent text-bg-primary rounded-lg text-xs font-medium hover:bg-accent-hover transition-colors"
            >
              Install CA
            </button>
          )}
        </div>
      </div>

      {loading ? (
        <div className="text-text-dim text-sm">Loading...</div>
      ) : certs.length === 0 ? (
        <div className="bg-bg-secondary rounded-lg p-8 border border-border text-center">
          <Shield size={32} className="mx-auto text-text-dim mb-3" />
          <p className="text-text-muted text-sm">No certificates generated</p>
          <p className="text-text-dim text-xs mt-2">
            Certificates are auto-generated when you create a project with SSL
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {certs.map(c => (
            <div
              key={c.domain}
              className="flex items-center justify-between px-4 py-3 bg-bg-secondary rounded-lg border border-border"
            >
              <div>
                <span className="text-sm font-medium font-mono">{c.domain}</span>
                <div className="flex items-center gap-3 mt-0.5">
                  <span className="text-xs text-text-dim">
                    Expires: {new Date(c.expires_at).toLocaleDateString()}
                  </span>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => handleRegenerate(c.domain)}
                  className="p-1.5 text-text-dim hover:text-text-primary transition-colors"
                  title="Regenerate certificate"
                >
                  <RefreshCw size={12} />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
