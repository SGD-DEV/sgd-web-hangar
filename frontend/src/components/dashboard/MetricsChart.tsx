interface MetricsChartProps {
  label: string
  value: number
  maxValue: number
  unit: string
}

export default function MetricsChart({ label, value, maxValue, unit }: MetricsChartProps) {
  const percent = maxValue > 0 ? (value / maxValue) * 100 : 0

  return (
    <div className="bg-bg-secondary rounded-lg p-3 border border-border">
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs text-text-muted">{label}</span>
        <span className="text-xs font-mono text-text-primary">
          {value}{unit}
        </span>
      </div>
      <div className="h-1.5 bg-bg-primary rounded-full overflow-hidden">
        <div
          className="h-full bg-accent rounded-full transition-all duration-500"
          style={{ width: `${Math.min(percent, 100)}%` }}
        />
      </div>
    </div>
  )
}
