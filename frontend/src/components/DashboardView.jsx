import { useEffect, useMemo, useRef, useState } from 'react'
import StatTile from './StatTile.jsx'
import VersionChart from './VersionChart.jsx'
import { fetchVersions } from '../api.js'

const POLL_MS = 5000

const RANGES = [
  ['1d', 'Last 1 day'],
  ['2d', 'Last 2 days'],
  ['3d', 'Last 3 days'],
  ['5d', 'Last 5 days'],
  ['1w', 'Last 1 week'],
  ['2w', 'Last 2 weeks'],
  ['1mo', 'Last 1 month'],
  ['3mo', 'Last 3 months'],
  ['1y', 'Last 1 year'],
  ['all', 'All time'],
]

// Renders one dashboard for a selected app: pulls per-version stats for the
// dashboard's metric and lays them out by kind — 'percentiles' (p99 full,
// p95/p90 half, like Latency) or 'single' (one value per version, like error
// rate / throughput).
export default function DashboardView({ cfg, apps, onBack, onAuthError }) {
  const [app, setApp] = useState(apps[0]?.name || '')
  const [range, setRange] = useState('1d')
  const [versions, setVersions] = useState([])
  const [selected, setSelected] = useState('')
  const selectedRef = useRef('')
  useEffect(() => {
    selectedRef.current = selected
  }, [selected])

  useEffect(() => {
    if (!app) return
    let alive = true
    const load = async () => {
      try {
        const vs = await fetchVersions(app, range, cfg.metric_key)
        if (!alive) return
        setVersions(vs)
        const names = vs.map((v) => v.version)
        if (vs.length === 0) setSelected('')
        else if (!selectedRef.current || !names.includes(selectedRef.current)) setSelected(vs[vs.length - 1].version)
      } catch (e) {
        if (alive) onAuthError?.(e)
      }
    }
    load()
    const id = setInterval(load, POLL_MS)
    return () => {
      alive = false
      clearInterval(id)
    }
  }, [app, range, cfg.metric_key])

  const selIdx = useMemo(() => versions.findIndex((v) => v.version === selected), [versions, selected])
  const selVer = selIdx >= 0 ? versions[selIdx] : null
  const prevVer = selIdx > 0 ? versions[selIdx - 1] : null

  const fmt = (v) => (v == null ? '—' : `${v.toFixed(cfg.decimals)}${cfg.unit}`)

  return (
    <div className="content-inner fade-in">
      <div className="section-label dash-view-head">
        <span className="dash-view-title">
          <button type="button" className="back-link" onClick={onBack}>
            ← All dashboards
          </button>
          <span className="sep">/</span>
          <span>{cfg.name}</span>
        </span>
        <div className="header-actions">
          <select className="pill" value={app} onChange={(e) => setApp(e.target.value)} aria-label="Select app">
            {apps.length === 0 && <option value="">no apps</option>}
            {apps.map((a) => (
              <option key={a.id} value={a.name}>
                {a.name}
              </option>
            ))}
          </select>
          <select className="pill" value={range} onChange={(e) => setRange(e.target.value)} aria-label="Time range">
            {RANGES.map(([v, label]) => (
              <option key={v} value={v}>
                {label}
              </option>
            ))}
          </select>
          <span className="pill live">
            <span className="dot" />
            live
          </span>
        </div>
      </div>

      {versions.length === 0 ? (
        <div className="empty" style={{ height: 220 }}>
          No {cfg.name.toLowerCase()} data for {app || 'this app'} yet — fire a deploy marker to populate it.
        </div>
      ) : (
        <>
          <div className="section-label">
            SELECTED VERSION · <span style={{ color: 'var(--accent)' }}>{selected || '—'}</span>
            <span className="section-hint">click a point on any chart to inspect a version</span>
          </div>

          {cfg.kind === 'percentiles' ? (
            <>
              <div className="tiles">
                {[
                  ['P50', selVer?.p50, prevVer?.p50],
                  ['P90', selVer?.p90, prevVer?.p90],
                  ['P95', selVer?.p95, prevVer?.p95],
                  ['P99', selVer?.p99, prevVer?.p99],
                ].map(([label, cur, prev]) => (
                  <StatTile key={label} label={label} {...deltaProps(cur, prev, fmt, prevVer)} />
                ))}
              </div>
              <Panel title={`${cfg.name} by version · p99`} hint="p99 per version — click a point to inspect">
                <VersionChart versions={versions} selected={selected} onSelect={setSelected} series="p99" />
              </Panel>
              <div className="chart-row">
                <Panel title={`${cfg.name} by version · p95`} hint="p95 per version">
                  <VersionChart versions={versions} selected={selected} onSelect={setSelected} series="p95" />
                </Panel>
                <Panel title={`${cfg.name} by version · p90`} hint="p90 per version">
                  <VersionChart versions={versions} selected={selected} onSelect={setSelected} series="p90" />
                </Panel>
              </div>
            </>
          ) : (
            <>
              <div className="tiles">
                <StatTile label={cfg.name} {...deltaProps(selVer?.p50, prevVer?.p50, fmt, prevVer)} />
              </div>
              <Panel title={`${cfg.name} by version`} hint={`${cfg.name.toLowerCase()} per version — click a point to inspect`}>
                <VersionChart versions={versions} selected={selected} onSelect={setSelected} series="p50" />
              </Panel>
            </>
          )}
        </>
      )}
    </div>
  )
}

// deltaProps builds the StatTile value + delta indicator from current/previous.
function deltaProps(cur, prev, fmt, prevVer) {
  let deltaText, deltaTone
  if (cur != null && prev != null && prev > 0) {
    const pct = ((cur - prev) / prev) * 100
    if (Math.abs(pct) < 0.5) {
      deltaText = '~0%'
      deltaTone = 'flat'
    } else {
      deltaText = `${pct > 0 ? '▲' : '▼'} ${Math.abs(pct).toFixed(0)}%`
      deltaTone = pct > 0 ? 'up' : 'down'
    }
  }
  return { value: fmt(cur), deltaText, deltaTone, sub: prevVer ? `vs ${prevVer.version}` : 'baseline' }
}

function Panel({ title, hint, children }) {
  return (
    <div className="panel">
      <div className="panel-head">
        <span className="panel-title">{title}</span>
        <span className="legend">
          <span className="swatch" />
          {hint}
        </span>
      </div>
      <div className="panel-body">{children}</div>
    </div>
  )
}
