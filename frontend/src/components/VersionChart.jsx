// The version-comparison chart: one point per app version (y = p99 latency),
// connected in deploy order. Hovering a point shows its version + percentiles;
// clicking selects that version (which drives the tiles + drill-down above/below).
//
// Regressed versions - where p99 jumped >20% over the previous version - are
// drawn in the danger color so the "which deploy hurt?" answer is visible at a glance.
import { useRef, useState } from 'react'

const W = 680
const H = 250
const PADL = 50
const PADR = 18
const PADT = 18
const PADB = 50

// Release time = when the version's first sample landed, shown in UTC (…Z).
function fmtReleaseUTC(iso) {
  if (!iso) return '-'
  return new Date(iso).toISOString().slice(0, 19).replace('T', ' ') + 'Z'
}

export default function VersionChart({ versions, selected, onSelect, series = 'p99' }) {
  const wrapRef = useRef(null)
  const [hover, setHover] = useState(null) // { idx, left, top }

  if (!versions || versions.length === 0) {
    return <div className="empty">no versions yet…</div>
  }

  const n = versions.length
  const iw = W - PADL - PADR
  const ih = H - PADT - PADB

  const vals = versions.map((v) => v[series])
  let min = Math.min(...vals)
  let max = Math.max(...vals)
  if (min === max) {
    min -= 1
    max += 1
  }
  const pad = (max - min) * 0.2
  min = Math.max(0, min - pad)
  max += pad

  const x = (i) => (n === 1 ? PADL + iw / 2 : PADL + (i / (n - 1)) * iw)
  const y = (v) => PADT + ih - ((v - min) / (max - min)) * ih

  const line = versions.map((v, i) => `${x(i).toFixed(1)},${y(v[series]).toFixed(1)}`).join(' ')
  const area = `${x(0).toFixed(1)},${(PADT + ih).toFixed(1)} ${line} ${x(n - 1).toFixed(1)},${(PADT + ih).toFixed(1)}`

  const isRegressed = (i) => i > 0 && versions[i][series] > versions[i - 1][series] * 1.2
  const selIdx = versions.findIndex((v) => v.version === selected)
  const yLabels = [max, (max + min) / 2, min]

  function nearestIdx(e) {
    const rect = wrapRef.current.getBoundingClientRect()
    const fx = (e.clientX - rect.left) / rect.width
    return { rect, idx: Math.max(0, Math.min(n - 1, Math.round(fx * (n - 1)))) }
  }
  function handleMove(e) {
    const { rect, idx } = nearestIdx(e)
    setHover({
      idx,
      left: (x(idx) / W) * rect.width,
      top: (y(versions[idx][series]) / H) * rect.height,
    })
  }
  function handleClick(e) {
    const { idx } = nearestIdx(e)
    onSelect(versions[idx].version)
  }

  return (
    <div
      className="vchart"
      ref={wrapRef}
      onMouseMove={handleMove}
      onMouseLeave={() => setHover(null)}
      onClick={handleClick}
    >
      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: 'auto', display: 'block' }} role="img" aria-label="Latency by version">
        <defs>
          <linearGradient id="vArea" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#63d397" stopOpacity="0.18" />
            <stop offset="100%" stopColor="#63d397" stopOpacity="0" />
          </linearGradient>
        </defs>

        {[0, 0.25, 0.5, 0.75, 1].map((f, k) => (
          <line key={k} x1={PADL} x2={W - PADR} y1={PADT + ih * f} y2={PADT + ih * f} stroke="#16191f" />
        ))}
        {yLabels.map((v, k) => (
          <text key={k} x="0" y={PADT + (ih * k) / 2 + 4} fontFamily="IBM Plex Mono, monospace" fontSize="10" fill="#4b5563">
            {Math.round(v)}ms
          </text>
        ))}

        {hover && (
          <line x1={x(hover.idx)} x2={x(hover.idx)} y1={PADT} y2={PADT + ih} stroke="#2a3140" strokeDasharray="3 4" />
        )}

        <polygon points={area} fill="url(#vArea)" />
        <polyline points={line} fill="none" stroke="#63d397" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />

        {versions.map((v, i) => {
          const sel = i === selIdx
          const hov = hover && hover.idx === i
          const color = isRegressed(i) ? '#e0897a' : '#63d397'
          return (
            <g key={v.version}>
              {sel && <circle cx={x(i)} cy={y(v[series])} r="7.5" fill="none" stroke={color} strokeOpacity="0.4" strokeWidth="2" />}
              <circle cx={x(i)} cy={y(v[series])} r={hov || sel ? 5 : 3.5} fill={color} />
              <text
                x={x(i)}
                y={H - 16}
                fontFamily="IBM Plex Mono, monospace"
                fontSize="9.5"
                fill={sel ? '#63d397' : '#6b7280'}
                textAnchor="end"
                transform={`rotate(-35 ${x(i)} ${H - 16})`}
              >
                {v.version}
              </text>
            </g>
          )
        })}
      </svg>

      {hover && (
        <div className="vtip" style={{ left: hover.left, top: hover.top }}>
          <div className="vtip-v">{versions[hover.idx].version}</div>
          <div className="vtip-r"><span>p90</span><b>{Math.round(versions[hover.idx].p90)}ms</b></div>
          <div className="vtip-r"><span>p95</span><b>{Math.round(versions[hover.idx].p95)}ms</b></div>
          <div className="vtip-r"><span>p99</span><b>{Math.round(versions[hover.idx].p99)}ms</b></div>
          <div className="vtip-r"><span>released</span><b>{fmtReleaseUTC(versions[hover.idx].first_ts)}</b></div>
          <div className="vtip-hint">click to inspect →</div>
        </div>
      )}
    </div>
  )
}
