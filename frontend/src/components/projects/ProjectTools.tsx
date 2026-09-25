import { useState, useEffect, useCallback } from 'react'
import { Copy, Download, Upload, RefreshCw, Send, Loader2 } from 'lucide-react'
import { call, toast, useAction, errorMessage, confirmDialog } from '../../lib/api'
import { Button, Modal, Field, inputCls } from '../ui/Controls'
import type { Project, ProjectDatabase, ProjectMail } from './ProjectList'

// Per-project dialogs: database, mail (SMTP) and git.

interface SetupResult {
  project: Project
  files: string[]
}

function writtenTo(files: string[]) {
  return files?.length ? `Eingetragen in ${files.map(f => f.split(/[\\/]/).pop()).join(', ')}` : undefined
}

function CopyRow({ label, value, secret }: { label: string; value: string; secret?: boolean }) {
  const [show, setShow] = useState(!secret)
  return (
    <div className="flex items-center gap-3 py-1.5 border-b border-border last:border-0">
      <span className="w-24 text-xs text-text-muted flex-shrink-0">{label}</span>
      <span className="flex-1 font-mono text-xs text-text-primary break-all select-text">
        {show ? value : '••••••••••••'}
      </span>
      {secret && (
        <button onClick={() => setShow(s => !s)} className="text-[11px] text-text-dim hover:text-text-primary">
          {show ? 'verbergen' : 'anzeigen'}
        </button>
      )}
      <button
        onClick={() => navigator.clipboard.writeText(value).then(() => toast.info(`${label} kopiert`)).catch(() => {})}
        className="text-text-dim hover:text-text-primary" title={`${label} kopieren`}>
        <Copy size={11} />
      </button>
    </div>
  )
}

// --- Database -------------------------------------------------------------------

export function DatabaseDialog({ project, onClose, onChanged }: {
  project: Project
  onClose: () => void
  onChanged: (p: Project) => void
}) {
  const isWP = project.framework === 'wordpress'
  const [db, setDb] = useState<ProjectDatabase | undefined>(project.database)
  const [dbType, setDbType] = useState('mysql')
  const [manual, setManual] = useState(false)
  const [form, setForm] = useState<ProjectDatabase>({ type: 'mysql', host: '127.0.0.1', port: 0, name: '', user: '', password: '' })

  const done = (r: SetupResult) => { setDb(r.project.database); onChanged(r.project); return r }

  const [create, creating] = useAction(
    async () => done(await call<SetupResult>('CreateProjectDatabase', project.name, dbType)),
    { success: r => `Datenbank angelegt. ${writtenTo(r.files) || ''}`, error: 'Datenbank konnte nicht angelegt werden' })

  const [assign, assigning] = useAction(
    async () => done(await call<SetupResult>('SetProjectDatabase', project.name, { ...form, port: Number(form.port) || 0 })),
    { success: r => `Datenbank zugewiesen. ${writtenTo(r.files) || ''}`, error: 'Datenbank konnte nicht zugewiesen werden' })

  const [detach, detaching] = useAction(
    async () => done(await call<SetupResult>('SetProjectDatabase', project.name, null)),
    { success: 'Zuordnung entfernt - die Datenbank selbst bleibt erhalten', error: 'Zuordnung konnte nicht entfernt werden' })

  const [rewrite, rewriting] = useAction(
    async () => done(await call<SetupResult>('SetProjectDatabase', project.name, db)),
    { success: r => writtenTo(r.files) || 'Gespeichert', error: 'Konfiguration konnte nicht geschrieben werden' })

  const target = isWP ? 'wp-config.php' : project.framework === 'proxy' ? null : '.env'

  return (
    <Modal title={`Datenbank - ${project.name}`} onClose={onClose}>
      {db ? (
        <div className="space-y-4">
          <div className="bg-bg-primary rounded-lg border border-border px-3 py-1">
            <CopyRow label="Typ" value={db.type === 'mysql' ? 'MySQL' : 'PostgreSQL'} />
            <CopyRow label="Host" value={db.host} />
            <CopyRow label="Port" value={String(db.port)} />
            <CopyRow label="Datenbank" value={db.name} />
            <CopyRow label="Benutzer" value={db.user} />
            <CopyRow label="Passwort" value={db.password} secret />
          </div>
          <p className="text-[11px] text-text-dim">
            {target
              ? <>Diese Werte stehen in der Datei <span className="font-mono">{target}</span> des Projekts. Einfaches PHP liest <span className="font-mono">.env</span> mit <span className="font-mono">parse_ini_file(__DIR__ . '/.env')</span>; der Webserver liefert sie nie aus.</>
              : project.app
                ? 'Die App bekommt diese Werte als Umgebungsvariablen (DB_HOST, DB_DATABASE, …, DATABASE_URL); ihr Dienst startet bei Änderungen neu.'
                : 'Für Proxy-Projekte schreibt Hangar keine Konfigurationsdatei - trage die Werte in die Konfiguration deiner App ein.'}
          </p>
          <div className="flex justify-between gap-2 pt-2 border-t border-border">
            <Button variant="danger" busy={detaching} onClick={async () => {
              if (await confirmDialog({
                title: 'Datenbank-Zuordnung entfernen?',
                message: 'Die Datenbank und ihre Daten bleiben erhalten - nur die Verknüpfung mit diesem Projekt wird entfernt.',
                confirmLabel: 'Zuordnung entfernen',
                danger: true,
              })) detach()
            }}>Zuordnung entfernen</Button>
            <div className="flex gap-2">
              {target && <Button busy={rewriting} onClick={rewrite}>{target} neu schreiben</Button>}
              <Button variant="primary" onClick={onClose}>Schließen</Button>
            </div>
          </div>
        </div>
      ) : !manual ? (
        <div className="space-y-4">
          <p className="text-sm text-text-muted">
            Legt eine Datenbank und einen Benutzer namens <span className="font-mono text-text-primary">{project.name.toLowerCase().replace(/[^a-z0-9_]+/g, '_')}</span> mit
            zufälligem Passwort an{target ? <> und trägt beides in <span className="font-mono text-text-primary">{target}</span> ein</> : null}.
          </p>
          {!isWP && (
            <Field label="Datenbankserver">
              <select className={inputCls} value={dbType} onChange={e => setDbType(e.target.value)}>
                <option value="mysql">MySQL</option>
                <option value="postgresql">PostgreSQL</option>
              </select>
            </Field>
          )}
          <div className="flex justify-between gap-2 pt-2 border-t border-border">
            <Button variant="ghost" onClick={() => setManual(true)}>Stattdessen eine vorhandene Datenbank verwenden…</Button>
            <Button variant="primary" busy={creating} onClick={create}>Datenbank anlegen</Button>
          </div>
        </div>
      ) : (
        <div className="space-y-3">
          <Field label="Datenbankserver">
            <select className={inputCls} value={form.type} onChange={e => setForm({ ...form, type: e.target.value })} disabled={isWP}>
              <option value="mysql">MySQL</option>
              <option value="postgresql">PostgreSQL</option>
            </select>
          </Field>
          <div className="grid grid-cols-3 gap-3">
            <div className="col-span-2"><Field label="Host"><input className={inputCls} value={form.host} onChange={e => setForm({ ...form, host: e.target.value })} /></Field></div>
            <Field label="Port" hint="leer = Standard"><input className={inputCls} value={form.port || ''} onChange={e => setForm({ ...form, port: Number(e.target.value.replace(/\D/g, '')) })} /></Field>
          </div>
          <Field label="Datenbank"><input className={inputCls} value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></Field>
          <Field label="Benutzer"><input className={inputCls} value={form.user} onChange={e => setForm({ ...form, user: e.target.value })} /></Field>
          <Field label="Passwort"><input type="password" className={inputCls} value={form.password} onChange={e => setForm({ ...form, password: e.target.value })} /></Field>
          <div className="flex justify-between gap-2 pt-2 border-t border-border">
            <Button variant="ghost" onClick={() => setManual(false)}>Zurück</Button>
            <Button variant="primary" busy={assigning} onClick={assign} disabled={!form.name || !form.user}>Zuweisen</Button>
          </div>
        </div>
      )}
    </Modal>
  )
}

// --- Mail -----------------------------------------------------------------------

const emptyMail: ProjectMail = { host: '', port: 587, user: '', password: '', encryption: 'tls', from_email: '', from_name: '' }

export function MailDialog({ project, onClose, onChanged }: {
  project: Project
  onClose: () => void
  onChanged: (p: Project) => void
}) {
  const [useMailpit, setUseMailpit] = useState(!project.mail)
  const [m, setM] = useState<ProjectMail>(project.mail || emptyMail)
  const [testTo, setTestTo] = useState('')
  const set = (patch: Partial<ProjectMail>) => setM(prev => ({ ...prev, ...patch }))
  const isWP = project.framework === 'wordpress'

  async function store() {
    const r = await call<SetupResult>('SetProjectMail', project.name, useMailpit ? null : { ...m, port: Number(m.port) || 0 })
    onChanged(r.project)
    return r
  }

  const [save, saving] = useAction(store, { success: r => `Mail-Einstellungen gespeichert. ${writtenTo(r.files) || ''}`, error: 'Mail-Einstellungen konnten nicht gespeichert werden' })

  const [test, testing] = useAction(async () => {
    await store()
    await call('SendProjectTestMail', project.name, testTo)
  }, {
    success: useMailpit ? 'Testmail gesendet - schau in Mailpit (http://127.0.0.1:8025)' : `Testmail an ${testTo} gesendet`,
    error: 'Testmail fehlgeschlagen',
  })

  function setEncryption(enc: string) {
    const port = enc === 'ssl' ? 465 : enc === 'tls' ? 587 : 25
    set({ encryption: enc, port: [25, 465, 587].includes(Number(m.port)) || !m.port ? port : m.port })
  }

  return (
    <Modal title={`Mail - ${project.name}`} onClose={onClose}>
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-2">
          {[true, false].map(v => (
            <button key={String(v)} onClick={() => setUseMailpit(v)}
              className={`text-left px-3 py-2 rounded-lg border text-xs transition-colors ${useMailpit === v ? 'border-accent/60 bg-accent/5 text-text-primary' : 'border-border text-text-muted hover:border-text-dim'}`}>
              <div className="font-medium text-sm">{v ? 'Mailpit (Test)' : 'SMTP-Server'}</div>
              <div className="text-[11px] text-text-dim mt-0.5">{v ? 'Mails werden lokal abgefangen, nichts wird zugestellt' : 'Echte Zustellung, z. B. über deinen Mail-Anbieter'}</div>
            </button>
          ))}
        </div>

        {!useMailpit && (
          <div className="space-y-3">
            <div className="grid grid-cols-3 gap-3">
              <div className="col-span-2"><Field label="SMTP-Host"><input className={inputCls} value={m.host} onChange={e => set({ host: e.target.value })} placeholder="smtp.example.com" /></Field></div>
              <Field label="Port"><input className={inputCls} value={m.port || ''} onChange={e => set({ port: Number(e.target.value.replace(/\D/g, '')) })} /></Field>
            </div>
            <Field label="Verschlüsselung">
              <select className={inputCls} value={m.encryption} onChange={e => setEncryption(e.target.value)}>
                <option value="tls">STARTTLS (meist Port 587)</option>
                <option value="ssl">SSL/TLS (meist Port 465)</option>
                <option value="">Keine</option>
              </select>
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Benutzer"><input className={inputCls} value={m.user} onChange={e => set({ user: e.target.value })} autoComplete="off" /></Field>
              <Field label="Passwort"><input type="password" className={inputCls} value={m.password} onChange={e => set({ password: e.target.value })} autoComplete="new-password" /></Field>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Absenderadresse" hint="Muss für das SMTP-Konto erlaubt sein"><input className={inputCls} value={m.from_email} onChange={e => set({ from_email: e.target.value })} placeholder="info@example.com" /></Field>
              <Field label="Absendername"><input className={inputCls} value={m.from_name} onChange={e => set({ from_name: e.target.value })} placeholder="Meine Seite" /></Field>
            </div>
          </div>
        )}

        <p className="text-[11px] text-text-dim">
          {isWP
            ? <>WordPress: wird in das Must-Use-Plugin <span className="font-mono">wp-content/mu-plugins/hangar-smtp.php</span> geschrieben - kein SMTP-Plugin nötig.</>
            : project.framework === 'proxy'
              ? (project.app
                ? 'Die App bekommt diese Werte als Umgebungsvariablen (MAIL_HOST, MAIL_PORT, …, MAILER_DSN); ihr Dienst startet bei Änderungen neu.'
                : 'Für Proxy-Projekte schreibt Hangar keine Konfigurationsdatei - verwende die Werte in deiner App.')
              : <>Wird in <span className="font-mono">.env</span> geschrieben als MAIL_HOST, MAIL_PORT, MAIL_USERNAME, … (Laravel-Namen). Einfaches PHP: <span className="font-mono">parse_ini_file(__DIR__ . '/.env')</span> und mit PHPMailer senden.</>}
        </p>

        <div className="flex items-end gap-2 pt-2 border-t border-border">
          <div className="flex-1">
            <Field label="Testmail senden an">
              <input className={inputCls} value={testTo} onChange={e => setTestTo(e.target.value)} placeholder="du@example.com" />
            </Field>
          </div>
          <Button busy={testing} disabled={!testTo.includes('@') || saving} onClick={test} icon={<Send size={12} />}>Speichern & testen</Button>
          <Button variant="primary" busy={saving} disabled={testing} onClick={async () => { if (await save()) onClose() }}>Speichern</Button>
        </div>
      </div>
    </Modal>
  )
}

// --- Git ------------------------------------------------------------------------

interface GitInfo {
  available: boolean
  is_repo: boolean
  branch: string
  remote: string
  has_upstream: boolean
  ahead: number
  behind: number
  changes: string[] | null
  last_commit: string
  user_name: string
  user_email: string
}

export function GitDialog({ project, onClose }: { project: Project; onClose: () => void }) {
  const [info, setInfo] = useState<GitInfo | null>(null)
  const [error, setError] = useState('')
  const [fetching, setFetching] = useState(false)
  const [message, setMessage] = useState('')
  const [remote, setRemote] = useState('')
  const [identity, setIdentity] = useState({ name: '', email: '' })
  const [output, setOutput] = useState('')

  const load = useCallback(async (fetch = false) => {
    setFetching(fetch)
    try {
      const i = await call<GitInfo>('GitStatus', project.name, fetch)
      setInfo(i)
      setRemote(i.remote || '')
      setError('')
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setFetching(false)
    }
  }, [project.name])

  useEffect(() => { load(false) }, [load])

  const [pull, pulling] = useAction(async () => {
    setOutput(await call<string>('GitPull', project.name))
    await load(false)
  }, { success: 'Pull erledigt', error: 'Pull fehlgeschlagen' })

  const [push, pushing] = useAction(async () => {
    setOutput(await call<string>('GitCommitPush', project.name, message))
    setMessage('')
    await load(false)
  }, { success: 'Push erledigt', error: 'Push fehlgeschlagen' })

  const [init, initing] = useAction(async () => {
    await call('GitInit', project.name, remote)
    await load(false)
  }, { success: 'Repository angelegt', error: 'Repository konnte nicht angelegt werden' })

  const [saveRemote, savingRemote] = useAction(async () => {
    await call('GitSetRemote', project.name, remote)
    await load(false)
  }, { success: 'Remote gespeichert', error: 'Remote konnte nicht gespeichert werden' })

  const [saveIdentity, savingIdentity] = useAction(async () => {
    await call('SetGitIdentity', identity.name, identity.email)
    await load(false)
  }, { success: 'Git-Identität gespeichert', error: 'Identität konnte nicht gespeichert werden' })

  const changes = info?.changes || []
  const busy = pulling || pushing

  return (
    <Modal title={`Git - ${project.name}`} onClose={onClose} width="max-w-2xl">
      {!info ? (
        error ? <div className="text-xs text-status-red">{error}</div>
          : <div className="text-text-dim text-sm flex items-center gap-2"><Loader2 size={12} className="animate-spin" /> Lädt…</div>
      ) : !info.available ? (
        <p className="text-sm text-text-muted">Git ist nicht installiert oder nicht im PATH.</p>
      ) : (
        <div className="space-y-4">
          {(!info.user_name || !info.user_email) && (
            <div className="rounded-lg border border-status-yellow/30 bg-status-yellow/5 p-3 space-y-2">
              <p className="text-xs text-status-yellow">Commits brauchen einen Namen und eine E-Mail-Adresse (wird in der globalen Git-Konfiguration gespeichert).</p>
              <div className="grid grid-cols-2 gap-2">
                <input className={inputCls} placeholder="Name" value={identity.name} onChange={e => setIdentity({ ...identity, name: e.target.value })} />
                <input className={inputCls} placeholder="E-mail" value={identity.email} onChange={e => setIdentity({ ...identity, email: e.target.value })} />
              </div>
              <Button busy={savingIdentity} disabled={!identity.name || !identity.email.includes('@')} onClick={saveIdentity}>Identität speichern</Button>
            </div>
          )}

          {!info.is_repo ? (
            <div className="space-y-3">
              <p className="text-sm text-text-muted">Dieses Projekt ist noch kein Git-Repository.</p>
              <Field label="Remote-Repository (optional)" hint="Ein leeres Repository auf GitHub, GitLab, Gitea, … - nur https. Beim ersten Push fragt Git nach deiner Anmeldung.">
                <input className={inputCls} value={remote} onChange={e => setRemote(e.target.value)} placeholder="https://github.com/user/repo.git" />
              </Field>
              <Button variant="primary" busy={initing} onClick={init}>Repository anlegen</Button>
            </div>
          ) : (
            <>
              <div className="bg-bg-primary rounded-lg border border-border px-3 py-2 text-xs space-y-1">
                <div className="flex justify-between gap-2">
                  <span>Branch <span className="font-mono text-accent">{info.branch || '(losgelöst)'}</span></span>
                  <span className="text-text-muted">
                    {info.has_upstream
                      ? <>{info.behind > 0 ? <span className="text-status-yellow">{info.behind} zum Pullen</span> : 'aktuell'} · {info.ahead > 0 ? <span className="text-status-yellow">{info.ahead} zum Pushen</span> : 'nichts zu pushen'}</>
                      : 'noch nicht gepusht'}
                  </span>
                </div>
                {info.last_commit && <div className="text-text-dim font-mono truncate" title={info.last_commit}>{info.last_commit}</div>}
              </div>

              <Field label="Remote (origin)">
                <div className="flex gap-2">
                  <input className={inputCls} value={remote} onChange={e => setRemote(e.target.value)} placeholder="https://github.com/user/repo.git" />
                  <Button busy={savingRemote} disabled={!remote || remote === info.remote} onClick={saveRemote}>Speichern</Button>
                </div>
              </Field>

              <div>
                <div className="text-xs text-text-muted mb-1">{changes.length ? `${changes.length} geänderte Datei${changes.length === 1 ? '' : 'en'}` : 'Keine lokalen Änderungen'}</div>
                {changes.length > 0 && (
                  <pre className="max-h-40 overflow-auto bg-bg-primary border border-border rounded-lg p-2 text-[11px] font-mono text-text-muted select-text">{changes.join('\n')}</pre>
                )}
              </div>

              {changes.length > 0 && (
                <Field label="Commit-Nachricht">
                  <input className={inputCls} value={message} onChange={e => setMessage(e.target.value)} placeholder="Was hat sich geändert?" />
                </Field>
              )}

              {output && <pre className="max-h-32 overflow-auto bg-bg-primary border border-border rounded-lg p-2 text-[11px] font-mono text-text-dim whitespace-pre-wrap select-text">{output}</pre>}

              <div className="flex justify-between gap-2 pt-2 border-t border-border">
                <Button busy={fetching} disabled={busy} onClick={() => load(true)} icon={<RefreshCw size={12} />} title="Beim Remote nach neuen Commits fragen">Fetch</Button>
                <div className="flex gap-2">
                  <Button busy={pulling} disabled={busy || !info.has_upstream} onClick={pull} icon={<Download size={12} />}
                    title="Auf den Remote-Stand vorspulen (lehnt ab, wenn beide Seiten geändert wurden)">Pull</Button>
                  <Button variant="primary" busy={pushing} disabled={busy || !info.remote || (changes.length > 0 && !message.trim()) || (changes.length === 0 && info.has_upstream && info.ahead === 0)}
                    onClick={push} icon={<Upload size={12} />}>
                    {changes.length ? 'Commit & Push' : 'Push'}
                  </Button>
                </div>
              </div>
              <p className="text-[11px] text-text-dim">
                .env, wp-config.php und das Hangar-Mail-Plugin werden nie committet (stehen in .git/info/exclude).
              </p>
            </>
          )}
        </div>
      )}
    </Modal>
  )
}
