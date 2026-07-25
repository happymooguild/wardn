import { useEffect, useMemo, useRef, useState } from 'react'
import Sidebar from './components/Sidebar.jsx'
import StatTile from './components/StatTile.jsx'
import VersionChart from './components/VersionChart.jsx'
import LatencyChart from './components/LatencyChart.jsx'
import Login from './components/Login.jsx'
import Deploys from './components/Deploys.jsx'
import Alerting from './components/Alerting.jsx'
import { fetchApps, fetchVersions, fetchVersionSeries, me, logout } from './api.js'

const POLL_MS = 5000

const RANGES = [
  ['1d', 'Last 1 day'],
  ['2d', 'Last 2 days'],
  ['3d', 'Last 3 days'],
  ['5d', 'Last 5 days'],
  ['1w', 'Last 1 week'],
  ['2w', 'Last 2 weeks'],
  ['1mo', 'Last 1 month'],
  ['2mo', 'Last 2 months'],
  ['3mo', 'Last 3 months'],
  ['1y', 'Last 1 year'],
  ['2y', 'Last 2 years'],
  ['all', 'All time'],
]

export default function App() {
  const [user, setUser] = useState(null)
  const [checking, setChecking] = useState(true)
  const [page, setPage] = useState('dashboards')

  const [apps, setApps] = useState([])
  const [app, setApp] = useState('')
  const [versions, setVersions] = useState([])
  const [selected, setSelected] = useState('')
  const [detail, setDetail] = useState([])
  const [range, setRange] = useState('1d')
  const [error, setError] = useState('')

  const selectedRef = useRef('')
  useEffect(() => {
    selectedRef.current = selected
  }, [selected])

  useEffect(() => {
    me()
      .then((u) => setUser(u))
      .catch(() => setUser(null))
      .finally(() => setChecking(false))
  }, [])

  const onAuthError = (e) => {
    if (String(e).includes('401')) setUser(null)
    else setError(String(e))
  }

  useEffect(() => {
    if (!user) return
    fetchApps()
      .then((list) => {
        setApps(list)
        if (list.length) setApp(list[0].name)
      })
      .catch(onAuthError)
  }, [user])

  // Poll per-version stats for the selected app + time range.
  useEffect(() => {
    if (!user || !app || page !== 'dashboards') return
    let alive = true
    const load = async () => {
      try {
        const vs = await fetchVersions(app, range)
        if (!alive) return
        setVersions(vs)
        setError('')
        // Default to (or fall back to) the latest in-range version when the
        // current selection is empty or has dropped out of the window.
        const names = vs.map((v) => v.version)
        if (vs.length === 0) {
          setSelected('')
        } else if (!selectedRef.current || !names.includes(selectedRef.current)) {
          setSelected(vs[vs.length - 1].version)
        }
      } catch (e) {
        if (alive) onAuthError(e)
      }
    }
    load()
    const id = setInterval(load, POLL_MS)
    return () => {
      alive = false
      clearInterval(id)
    }
  }, [user, app, page, range])

  // Poll raw samples for the selected version, within the time range.
  useEffect(() => {
    if (!user || !app || !selected || page !== 'dashboards') return
    let alive = true
    const load = async () => {
      try {
        const pts = await fetchVersionSeries(app, selected, range)
        if (alive) setDetail(pts)
      } catch (e) {
        if (alive) onAuthError(e)
      }
    }
    load()
    const id = setInterval(load, POLL_MS)
    return () => {
      alive = false
      clearInterval(id)
    }
  }, [user, app, selected, page, range])

  const selIdx = useMemo(() => versions.findIndex((v) => v.version === selected), [versions, selected])
  const selVer = selIdx >= 0 ? versions[selIdx] : null
  const prevVer = selIdx > 0 ? versions[selIdx - 1] : null
  const rawStats = useMemo(() => computeStats(detail), [detail])

  async function handleLogout() {
    await logout()
    setUser(null)
    setApps([])
    setApp('')
    setVersions([])
    setSelected('')
    selectedRef.current = ''
    setDetail([])
    setError('')
  }

  if (checking) return <div className="login-wrap" />
  if (!user) return <Login onSuccess={setUser} />

  const crumb =
    page === 'deploys' ? 'Deploys' : page === 'alerting' ? 'Alerting' : 'Latency by version'

  const pctTiles = [
    ['P50', selVer?.p50, prevVer?.p50],
    ['P90', selVer?.p90, prevVer?.p90],
    ['P95', selVer?.p95, prevVer?.p95],
    ['P99', selVer?.p99, prevVer?.p99],
  ]

  return (
    <div className="app">
      <Sidebar user={user} page={page} onNavigate={setPage} onLogout={handleLogout} />

      <div className="main">
        <header className="header">
          <div className="crumbs">
            <span>Home</span>
            <span className="sep">/</span>
            <span className="here">{crumb}</span>
          </div>
          <div className="header-main">
            <div className="header-title">
              <h1>Latency by version</h1>
              <span className="header-sub">p99 across every deploy{app ? ` · ${app}` : ''}</span>
            </div>
            <div className="header-controls">
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
        </header>

        <div className="content">
          {page === 'deploys' && <Deploys app={app} onAuthError={onAuthError} />}
          {page === 'alerting' && <Alerting apps={apps} appName={app} onAuthError={onAuthError} />}
          {page === 'dashboards' && (
            <div className="content-inner fade-in">
              <div className="section-label">
                SELECTED VERSION · <span style={{ color: 'var(--accent)' }}>{selected || '—'}</span>
              </div>
              <div className="tiles">
                {pctTiles.map(([label, cur, prev]) => {
                  const value = cur != null ? `${Math.round(cur)}ms` : '—'
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
                  return (
                    <StatTile
                      key={label}
                      label={label}
                      value={value}
                      deltaText={deltaText}
                      deltaTone={deltaTone}
                      sub={prevVer ? `vs ${prevVer.version}` : 'baseline'}
                    />
                  )
                })}
              </div>

              <div className="panel">
                <div className="panel-head">
                  <span className="panel-title">Latency by version · p99</span>
                  <span className="legend">
                    <span className="swatch" />
                    p99 per version — click a point to inspect
                  </span>
                </div>
                <div className="panel-body">
                  <VersionChart versions={versions} selected={selected} onSelect={setSelected} />
                </div>
              </div>

              <div className="panel">
                <div className="panel-head">
                  <span className="panel-title">
                    Selected ·{' '}
                    <span style={{ color: 'var(--accent)', fontFamily: 'var(--font-mono)' }}>{selected || '—'}</span> ·
                    latency over time
                  </span>
                  <span className="legend">
                    <span className="swatch" />
                    latency_ms
                  </span>
                </div>
                <div className="panel-body">
                  <LatencyChart points={detail} />
                </div>
              </div>

              <div className="tiles">
                <StatTile label="LATEST" value={rawStats.latest} sub={selected || '—'} />
                <StatTile label="AVERAGE" value={rawStats.avg} sub="mean of samples" />
                <StatTile label="PEAK" value={rawStats.max} sub="max sample" />
                <StatTile label="SAMPLES" value={rawStats.count} sub="data points" />
              </div>

              {error && (
                <div className="empty" style={{ height: 'auto', color: 'var(--danger)' }}>
                  {error} — is the backend running?
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function computeStats(points) {
  if (!points || points.length === 0) {
    return { latest: '—', avg: '—', max: '—', count: '0' }
  }
  const values = points.map((p) => p.value)
  const latest = values[values.length - 1]
  const avg = values.reduce((a, b) => a + b, 0) / values.length
  const max = Math.max(...values)
  return {
    latest: `${latest.toFixed(1)}ms`,
    avg: `${avg.toFixed(1)}ms`,
    max: `${max.toFixed(1)}ms`,
    count: String(points.length),
  }
}
