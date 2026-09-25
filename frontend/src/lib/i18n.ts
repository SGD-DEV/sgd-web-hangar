// German labels for values that come from the backend in English.

const statusLabels: Record<string, string> = {
  running: 'läuft',
  stopped: 'gestoppt',
  starting: 'startet',
  stopping: 'stoppt',
  error: 'Fehler',
  unknown: 'unbekannt',
  'not-installed': 'nicht installiert',
  // package downloads
  pending: 'wartet',
  downloading: 'lädt herunter',
  extracting: 'entpackt',
  verifying: 'prüft',
  installed: 'installiert',
  active: 'aktiv',
}

export function statusLabel(status?: string) {
  return statusLabels[status || 'unknown'] || status || 'unbekannt'
}
