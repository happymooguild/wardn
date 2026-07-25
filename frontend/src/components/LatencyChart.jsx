// Hand-built responsive SVG line chart — matches the design's chart language
// (mono axis labels, faint grid, accent line over a soft area gradient, a dot
// on the latest point). No charting library; it scales via viewBox.

const W = 680
const H = 260
const PADL = 50
const PADR = 18
const PADT = 18
const PADB = 34

function fmtTime(iso) {
  const d = new Date(iso)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export default function LatencyChart({ points }) {
  if (!points || points.length < 2) {
    return (
      <div className="empty">
        <span className="pill live"><span className="dot" /> live</span>
        waiting for samples…
      </div>
    )
  }

  const iw = W - PADL - PADR
  const ih = H - PADT - PADB

  const values = points.map((p) => p.value)
  let min = Math.min(...values)
  let max = Math.max(...values)
  if (min === max) {
    min -= 1
    max += 1
  }
  // Pad the range so the line doesn't hug the panel edges.
  const pad = (max - min) * 0.15
  min = Math.max(0, min - pad)
  max = max + pad

  const n = points.length
  const x = (i) => PADL + (i / (n - 1)) * iw
  const y = (v) => PADT + ih - ((v - min) / (max - min)) * ih

  const line = points.map((p, i) => `${x(i).toFixed(1)},${y(p.value).toFixed(1)}`).join(' ')
  const area = `${PADL},${(PADT + ih).toFixed(1)} ${line} ${x(n - 1).toFixed(1)},${(PADT + ih).toFixed(1)}`

  const gridFractions = [0, 0.25, 0.5, 0.75, 1]
  const yLabels = [max, (max + min) / 2, min]

  const last = points[n - 1]
  const xTickIdx = [0, Math.floor((n - 1) / 2), n - 1]

  return (
    <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: 'auto' }} role="img" aria-label="Latency over time">
      <defs>
        <linearGradient id="wdArea" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="#63d397" stopOpacity="0.22" />
          <stop offset="100%" stopColor="#63d397" stopOpacity="0" />
        </linearGradient>
      </defs>

      {/* grid */}
      {gridFractions.map((f, k) => (
        <line key={k} x1={PADL} x2={W - PADR} y1={PADT + ih * f} y2={PADT + ih * f} stroke="#16191f" />
      ))}

      {/* y-axis labels */}
      {yLabels.map((v, k) => (
        <text
          key={k}
          x="0"
          y={PADT + (ih * k) / 2 + 4}
          fontFamily="IBM Plex Mono, monospace"
          fontSize="10"
          fill="#4b5563"
        >
          {Math.round(v)}ms
        </text>
      ))}

      {/* x-axis labels */}
      {xTickIdx.map((i, k) => (
        <text
          key={i}
          x={x(i)}
          y={H - 8}
          fontFamily="IBM Plex Mono, monospace"
          fontSize="10"
          fill="#4b5563"
          textAnchor={k === 0 ? 'start' : k === xTickIdx.length - 1 ? 'end' : 'middle'}
        >
          {fmtTime(points[i].ts)}
        </text>
      ))}

      {/* area + line */}
      <polygon points={area} fill="url(#wdArea)" />
      <polyline
        points={line}
        fill="none"
        stroke="#63d397"
        strokeWidth="2.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />

      {/* latest point */}
      <circle cx={x(n - 1)} cy={y(last.value)} r="4" fill="#63d397" />
    </svg>
  )
}
