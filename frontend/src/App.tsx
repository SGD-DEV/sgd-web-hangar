import { useState, useEffect } from 'react'
import Sidebar from './components/layout/Sidebar'
import TopBar from './components/layout/TopBar'
import StatusBar from './components/layout/StatusBar'
import ServiceCard from './components/dashboard/ServiceCard'
import ServiceControls from './components/dashboard/ServiceControls'
import PHPVersionList from './components/php/PHPVersionList'
import ProjectList from './components/projects/ProjectList'
import SSLManager from './components/ssl/SSLManager'
import LogViewer from './components/services/LogViewer'
import ConfigEditor from './components/services/ConfigEditor'
import AppSettings from './components/settings/AppSettings'
import PackageManager from './components/packages/PackageManager'
import InstalledPage from './components/installed/InstalledPage'
import DevourTerminal from './components/terminal/Terminal'
import DatabasesPage from './components/databases/DatabasesPage'
import SystemPathManager from './components/syspath/SystemPathManager'
import WelcomeWizard from './components/onboarding/WelcomeWizard'
import TunnelPage from './components/tunnel/TunnelPage'
import WebServerSwitch from './components/dashboard/WebServerSwitch'
import Toaster from './components/ui/Toaster'

export type NavItem = 'servers' | 'packages' | 'installed' | 'php' | 'projects' | 'tunnel' | 'ssl' | 'databases' | 'terminal' | 'syspath' | 'settings'
export type DetailTab = 'overview' | 'config' | 'logs'

interface ServiceStatus {
  name: string
  status: 'running' | 'stopped' | 'starting' | 'error'
  port: number
  version: string
  pid: number
  uptime: string
  started_at: string
  error?: string
}

function App() {
  const [activeNav, setActiveNav] = useState<NavItem>('servers')
  const [activeTab, setActiveTab] = useState<DetailTab>('overview')
  const [selectedService, setSelectedService] = useState<string>('apache')
  const [services, setServices] = useState<Record<string, ServiceStatus>>({})
  const [showWizard, setShowWizard] = useState<boolean>(false)
  const [firstRunChecked, setFirstRunChecked] = useState<boolean>(false)

  // First-run check: ask the backend whether any runtimes are installed yet.
  // If not, show the welcome wizard before the rest of the UI is reachable.
  useEffect(() => {
    // @ts-ignore Wails bindings may not exist in dev
    const isFirstRun = window.go?.app?.App?.IsFirstRun
    if (!isFirstRun) {
      setFirstRunChecked(true)
      return
    }
    isFirstRun()
      .then((first: boolean) => {
        setShowWizard(!!first)
        setFirstRunChecked(true)
      })
      .catch(() => setFirstRunChecked(true))
  }, [])

  useEffect(() => {
    loadServices()
    const interval = setInterval(loadServices, 3000)
    return () => clearInterval(interval)
  }, [])

  async function loadServices() {
    try {
      // @ts-ignore - Wails bindings
      if (window.go?.app?.App?.GetAllServiceStatuses) {
        // @ts-ignore
        const statuses = await window.go.app.App.GetAllServiceStatuses()
        setServices(statuses || {})
      }
    } catch (e) {
      // App not connected yet
    }
  }

  const serviceList = [
    'apache', 'nginx', 'caddy',
    'mysql', 'postgresql', 'mongodb',
    'meilisearch',
    'mailpit',
  ]

  function renderMainContent() {
    switch (activeNav) {
      case 'servers':
        return (
          <div className="flex flex-1 h-full overflow-hidden">
            {/* Center: Service List */}
            <div className="w-80 border-r border-border flex-shrink-0 overflow-y-auto">
              <div className="p-4">
                <WebServerSwitch statuses={services} />
                <h2 className="text-xs font-medium text-text-muted uppercase tracking-wider mb-3">Services</h2>
                <div className="space-y-1">
                  {serviceList.map(name => (
                    <ServiceCard
                      key={name}
                      name={name}
                      status={services[name]}
                      selected={selectedService === name}
                      onClick={() => setSelectedService(name)}
                    />
                  ))}
                </div>
              </div>
            </div>

            {/* Right: Detail Panel */}
            <div className="flex-1 flex flex-col overflow-hidden">
              <div className="border-b border-border">
                <TopBar
                  tabs={['overview', 'config', 'logs']}
                  activeTab={activeTab}
                  onTabChange={(tab) => setActiveTab(tab as DetailTab)}
                />
              </div>
              <div className="flex-1 overflow-y-auto p-6">
                {activeTab === 'overview' && (
                  <ServiceControls
                    key={selectedService}
                    serviceName={selectedService}
                    status={services[selectedService]}
                  />
                )}
                {activeTab === 'logs' && (
                  <LogViewer serviceName={selectedService} />
                )}
                {activeTab === 'config' && (
                  <ConfigEditor serviceName={selectedService} />
                )}
              </div>
            </div>
          </div>
        )

      case 'packages':
        return <PackageManager />

      case 'installed':
        return <InstalledPage />

      case 'php':
        return <PHPVersionList onNavigate={(nav) => setActiveNav(nav as NavItem)} />

      case 'projects':
        return <ProjectList />

      case 'tunnel':
        return <TunnelPage />

      case 'ssl':
        return <SSLManager />

      case 'databases':
        return <DatabasesPage services={services} />

      case 'terminal':
        return <DevourTerminal />

      case 'syspath':
        return <SystemPathManager />

      case 'settings':
        return <AppSettings />

      default:
        return null
    }
  }

  return (
    <div className="h-screen w-screen flex flex-col bg-bg-primary text-text-primary overflow-hidden select-none">
      {/* Custom Titlebar */}
      <div
        className="h-9 flex items-center justify-between px-4 bg-bg-sidebar border-b border-border flex-shrink-0"
        style={{ WebkitAppRegion: 'drag' } as React.CSSProperties}
      >
        <div className="flex items-center gap-2">
          <span className="text-accent font-medium text-sm tracking-tight">▪ HANGAR</span>
        </div>
        <div
          className="flex items-center gap-1"
          style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
        >
          <button
            onClick={() => { /* @ts-ignore */ window.go?.app?.App?.MinimiseWindow?.() }}
            className="w-7 h-7 flex items-center justify-center rounded hover:bg-bg-secondary text-text-muted hover:text-text-primary transition-colors"
          >
            ─
          </button>
          <button
            onClick={() => { /* @ts-ignore */ window.go?.app?.App?.MaximiseWindow?.() }}
            className="w-7 h-7 flex items-center justify-center rounded hover:bg-bg-secondary text-text-muted hover:text-text-primary transition-colors"
          >
            □
          </button>
          <button
            onClick={() => { /* @ts-ignore */ window.go?.app?.App?.CloseWindow?.() }}
            className="w-7 h-7 flex items-center justify-center rounded hover:bg-status-red/20 text-text-muted hover:text-status-red transition-colors"
          >
            ×
          </button>
        </div>
      </div>

      {/* Main Layout */}
      <div className="flex flex-1 overflow-hidden">
        {/* Sidebar */}
        <Sidebar activeNav={activeNav} onNavChange={setActiveNav} />

        {/* Content */}
        <div className="flex-1 flex flex-col overflow-hidden">
          {renderMainContent()}
        </div>
      </div>

      {/* Status Bar */}
      <StatusBar services={services} />

      {/* First-run welcome wizard. Shown over everything else until the user
          either installs the default bundle or skips. The first-run check
          itself is gated on firstRunChecked so we don't flash the wizard
          for one frame on returning installs. */}
      {firstRunChecked && showWizard && (
        <WelcomeWizard onComplete={() => setShowWizard(false)} />
      )}

      <Toaster />
    </div>
  )
}

export default App
