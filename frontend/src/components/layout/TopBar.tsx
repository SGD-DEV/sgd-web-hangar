interface TopBarProps {
  tabs: string[]
  activeTab: string
  onTabChange: (tab: string) => void
}

export default function TopBar({ tabs, activeTab, onTabChange }: TopBarProps) {
  return (
    <div className="flex items-center px-4 h-10 gap-1">
      {tabs.map(tab => (
        <button
          key={tab}
          onClick={() => onTabChange(tab)}
          className={`px-3 py-1.5 text-xs font-medium rounded transition-colors capitalize
            ${activeTab === tab
              ? 'text-accent bg-bg-selected'
              : 'text-text-muted hover:text-text-primary hover:bg-bg-secondary'
            }`}
        >
          {tab}
        </button>
      ))}
    </div>
  )
}
