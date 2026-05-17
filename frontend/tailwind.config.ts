import type { Config } from 'tailwindcss'

export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        'bg-primary': '#0f1010',
        'bg-secondary': '#1a1c1a',
        'bg-sidebar': '#111211',
        'bg-selected': '#1e2a1e',
        accent: '#b5f23d',
        'accent-hover': '#c8ff4a',
        'text-primary': '#e8e8e6',
        'text-muted': '#6b7068',
        'text-dim': '#3d4039',
        border: '#252720',
        'status-green': '#4ade80',
        'status-red': '#f87171',
        'status-yellow': '#fbbf24',
      },
      fontFamily: {
        sans: ['Geist', 'system-ui', 'sans-serif'],
        mono: ['Geist Mono', 'JetBrains Mono', 'monospace'],
      },
      fontSize: {
        'xs': '11px',
        'sm': '12px',
        'base': '13px',
        'lg': '14px',
      },
      animation: {
        'pulse-slow': 'pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
      },
    },
  },
  plugins: [],
} satisfies Config
