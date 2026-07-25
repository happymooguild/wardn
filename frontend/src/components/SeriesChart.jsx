// Raw metric series chart: one line, x = time, y = value. Used by Explore to
// plot the actual samples behind a version's percentiles (VersionChart, by
// contrast, plots one aggregated point per version).
import { useRef, useState } from 'react'

const W = 680
const H = 220
const PADL = 54
const PADR = 18
const PADT = 18
const PADB = 34

export default function SeriesChart({ points }) {
  const wrapRef = useRef(null)
  const [hover, setHover] = useState(null)

  if (!points || points.length === 0) {
    return <div className="empty">no samples in range…</div>
  }

  const n = points.length
  const iw = W - PADL - PADR
  const ih = H - PADT - PADB

  const times = points.map((p) => new Date(p.ts).getTime())
  const vals = points.map((p) => p.value)
  const minT = Math.min(...times)
  const maxT = Math.max(...times)
  let min = Math.min(...vals)
  let max = Math.max(...vals)
  if (min === max) {
    min -= 1
    max += 1
  }
  const pad = (max - min) * 0.15
  min = Math.max(0, min - pad)
  max += pad

  const x = (t) => (maxT === minT ? PADL + iw / 2 : PADL + ((t - minT) / (maxT - minT)) * iw)
  const y = (v) => PADT + ih - ((v - min) / (max - min)) * ih

  const line = points.map((p, i) => `${x(times[i]).toFixed(1)},${y(p.value).toFixed(1)}`).join(' ')
  const yLabels = [max, (max + min) / 2, min]

  function nearestIdx(e) {
    const rect = wrapRef.current.getBoundingClientRect()
    const fx = (e.clientX - rect.left) / rect.width
    const t = minT + fx * (maxT - minT)
    let best = 0
    let bestDist = Infinity
    times.forEach((ti, i) => {
      const d = Math.abs(ti - t)
      if (d < bestDist) {
        bestDist = d
        best = i
      }
    })
    return { rect, idx: best }
  }
  function handleMove(e) {
    const { rect, idx } = nearestIdx(e)
    setHover({
      idx,
      left: (x(times[idx]) / W) * rect.width,
      top: (y(points[idx].value) / H) * rect.height,
    })
  }

  return (
    <div className="vchart" ref={wrapRef} onMouseMove={handleMove} onMouseLeave={() => setHover(null)}>
      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: 'auto', display: 'block' }} role="img" aria-label="Raw metric series">
        {[0, 0.25, 0.5, 0.75, 1].map((f, k) => (
          <line key={k} x1={PADL} x2={W - PADR} y1={PADT + ih * f} y2={PADT + ih * f} stroke="#16191f" />
        ))}
        {yLabels.map((v, k) => (
          <text key={k} x="0" y={PADT + (ih * k) / 2 + 4} fontFamily="IBM Plex Mono, monospace" fontSize="10" fill="#4b5563">
            {v.toFixed(1)}
          </text>
        ))}

        {hover && <line x1={x(times[hover.idx])} x2={x(times[hover.idx])} y1={PADT} y2={PADT + ih} stroke="#2a3140" strokeDasharray="3 4" />}

        <polyline points={line} fill="none" stroke="#63d397" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />

        {hover != null && (
          <circle cx={x(times[hover.idx])} cy={y(points[hover.idx].value)} r="4" fill="#63d397" />
        )}

        <text x={PADL} y={H - 10} fontFamily="IBM Plex Mono, monospace" fontSize="9.5" fill="#6b7280">
          {new Date(minT).toLocaleString()}
        </text>
        <text x={W - PADR} y={H - 10} textAnchor="end" fontFamily="IBM Plex Mono, monospace" fontSize="9.5" fill="#6b7280">
          {new Date(maxT).toLocaleString()}
        </text>
      </svg>

      {hover && (
        <div className="vtip" style={{ left: hover.left, top: hover.top }}>
          <div className="vtip-v">{points[hover.idx].value.toFixed(2)}</div>
          <div className="vtip-r">
            <span>{new Date(points[hover.idx].ts).toLocaleString()}</span>
          </div>
        </div>
      )}
    </div>
  )
}
