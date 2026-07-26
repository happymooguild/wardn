// A visual showcase of what wardn does - illustrated feature cards rather
// than a data table. Each card doubles as a shortcut into the real page.
const FEATURES = [
  {
    page: 'dashboards',
    title: 'Latency by version',
    desc: 'Every deploy plotted as a point - p50/p90/p95/p99 side by side, with a time-range selector and click-to-inspect drill-down.',
    art: DashboardsArt,
  },
  {
    page: 'deploys',
    title: 'Before / after snapshots',
    desc: 'Each deploy marker captures a before-window and an after-window, so a regression is a comparison, not a guess.',
    art: DeploysArt,
  },
  {
    page: 'alerting',
    title: 'Regression alerts',
    desc: 'The moment a deploy regresses, wardn fires to Slack or a generic webhook - with delivery history so you know what actually went out.',
    art: AlertingArt,
  },
  {
    page: 'ai',
    title: 'AI root cause',
    desc: 'Ask AI reasons over the before/after metrics plus a bounded sample of error logs and slow traces, and explains why - not just that.',
    art: AiArt,
  },
  {
    page: 'deploys',
    title: 'Deploy-aware pipeline',
    desc: 'CI/ArgoCD marks a deploy, the analyzer waits out the after-window, then queries SigNoz - fully automatic, no dashboards to babysit.',
    art: PipelineArt,
  },
]

export default function Explore({ onNavigate }) {
  return (
    <div className="content-inner fade-in">
      <div className="hero panel">
        <div className="hero-kicker">EXPLORE · WHAT WARDN CAN DO</div>
        <h2 className="hero-title">One tool, the whole deploy story</h2>
        <p className="hero-sub">
          From the moment CI marks a release to the moment a regression pings Slack - here's every
          piece of the pipeline, and where to find it.
        </p>
      </div>

      <div className="showcase">
        {FEATURES.map((f, i) => (
          <button key={`${f.page}-${i}`} className="showcase-card" type="button" onClick={() => onNavigate?.(f.page)}>
            <div className="showcase-art">
              <f.art />
            </div>
            <div className="showcase-title">{f.title}</div>
            <div className="showcase-desc">{f.desc}</div>
            <div className="card-arrow">Open →</div>
          </button>
        ))}
      </div>
    </div>
  )
}

function DashboardsArt() {
  return (
    <svg width="100%" height="96" viewBox="0 0 200 96" fill="none">
      <line x1="16" y1="76" x2="184" y2="76" stroke="#1d222a" />
      {[0, 1, 2, 3].map((i) => (
        <line key={i} x1={16 + i * 56} y1="10" x2={16 + i * 56} y2="76" stroke="#16191f" />
      ))}
      <polygon points="16,60 72,44 128,50 184,20 184,76 16,76" fill="#63d397" opacity="0.12" />
      <polyline points="16,60 72,44 128,50 184,20" fill="none" stroke="#63d397" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="16" cy="60" r="3.5" fill="#63d397" />
      <circle cx="72" cy="44" r="3.5" fill="#63d397" />
      <circle cx="128" cy="50" r="3.5" fill="#e0897a" />
      <circle cx="184" cy="20" r="3.5" fill="#63d397" />
    </svg>
  )
}

function DeploysArt() {
  return (
    <svg width="100%" height="96" viewBox="0 0 200 96" fill="none">
      <line x1="16" y1="80" x2="184" y2="80" stroke="#1d222a" />
      <rect x="40" y="46" width="26" height="34" rx="3" fill="#2a3140" />
      <rect x="134" y="24" width="26" height="56" rx="3" fill="#e0897a" opacity="0.85" />
      <text x="53" y="40" textAnchor="middle" fontFamily="IBM Plex Mono, monospace" fontSize="9" fill="#6b7280">before</text>
      <text x="147" y="18" textAnchor="middle" fontFamily="IBM Plex Mono, monospace" fontSize="9" fill="#e0897a">after</text>
      <path d="M70 55L128 45" stroke="#4b5563" strokeWidth="1.4" strokeDasharray="3 3" markerEnd="url(#arrow)" />
      <defs>
        <marker id="arrow" markerWidth="8" markerHeight="8" refX="4" refY="4" orient="auto">
          <path d="M0,0 L8,4 L0,8 Z" fill="#4b5563" />
        </marker>
      </defs>
      <text x="100" y="38" textAnchor="middle" fontFamily="IBM Plex Mono, monospace" fontSize="9" fill="#e0897a">▲ 62%</text>
    </svg>
  )
}

function AlertingArt() {
  return (
    <svg width="100%" height="96" viewBox="0 0 200 96" fill="none">
      <circle cx="34" cy="48" r="18" stroke="#e0897a" strokeWidth="1.6" />
      <path d="M34 40V50" stroke="#e0897a" strokeWidth="1.8" strokeLinecap="round" />
      <circle cx="34" cy="56" r="1.6" fill="#e0897a" />
      <path d="M54 44C80 34 90 34 116 40" stroke="#4b5563" strokeWidth="1.3" strokeDasharray="2 4" />
      <path d="M54 52C80 58 90 60 116 58" stroke="#4b5563" strokeWidth="1.3" strokeDasharray="2 4" />
      <rect x="118" y="26" width="46" height="26" rx="6" stroke="#63d397" strokeWidth="1.5" />
      <text x="141" y="42" textAnchor="middle" fontFamily="IBM Plex Mono, monospace" fontSize="9" fill="#63d397">Slack</text>
      <rect x="118" y="56" width="46" height="26" rx="6" stroke="#8a9099" strokeWidth="1.5" />
      <text x="141" y="72" textAnchor="middle" fontFamily="IBM Plex Mono, monospace" fontSize="8.5" fill="#8a9099">webhook</text>
    </svg>
  )
}

function AiArt() {
  const inputs = [
    ['metrics', 14],
    ['logs', 48],
    ['traces', 82],
  ]
  return (
    <svg width="100%" height="96" viewBox="0 0 200 96" fill="none">
      {inputs.map(([label, y]) => (
        <g key={label}>
          <rect x="10" y={y - 10} width="52" height="20" rx="5" stroke="#22262e" strokeWidth="1.4" />
          <text x="36" y={y + 4} textAnchor="middle" fontFamily="IBM Plex Mono, monospace" fontSize="8.5" fill="#9aa1ab">{label}</text>
          <path d={`M62 ${y}C90 ${y} 90 48 108 48`} stroke="#4b5563" strokeWidth="1.2" strokeDasharray="2 3" />
        </g>
      ))}
      <path d="M118 40L121.6 45.6L128 48L121.6 50.4L118 56L114.4 50.4L108 48L114.4 45.6L118 40Z" fill="#63d397" />
      <path d="M148 30L149.8 34.4L154 36L149.8 37.6L148 42L146.2 37.6L142 36L146.2 34.4L148 30Z" fill="#63d397" opacity="0.7" />
      <path d="M132 48C150 48 158 48 172 48" stroke="#63d397" strokeWidth="1.4" strokeDasharray="1 4" />
      <rect x="172" y="38" width="24" height="20" rx="5" stroke="#63d397" strokeWidth="1.5" />
    </svg>
  )
}

function PipelineArt() {
  const steps = ['CI', 'Marker', 'Analyzer', 'SigNoz']
  const w = 200 / steps.length
  return (
    <svg width="100%" height="96" viewBox="0 0 200 96" fill="none">
      {steps.map((s, i) => {
        const cx = w * i + w / 2
        return (
          <g key={s}>
            <rect x={cx - 26} y="38" width="52" height="26" rx="6" stroke={i === 2 ? '#63d397' : '#22262e'} strokeWidth="1.5" />
            <text x={cx} y="54" textAnchor="middle" fontFamily="IBM Plex Mono, monospace" fontSize="9" fill={i === 2 ? '#63d397' : '#9aa1ab'}>
              {s}
            </text>
            {i < steps.length - 1 && (
              <path d={`M${cx + 26} 51L${cx + w - 26} 51`} stroke="#4b5563" strokeWidth="1.3" markerEnd="url(#parrow)" />
            )}
          </g>
        )
      })}
      <defs>
        <marker id="parrow" markerWidth="8" markerHeight="8" refX="4" refY="4" orient="auto">
          <path d="M0,0 L8,4 L0,8 Z" fill="#4b5563" />
        </marker>
      </defs>
    </svg>
  )
}
