import { useEffect, useMemo, useRef, useState } from 'react'
import Sidebar from './components/Sidebar.jsx'
import StatTile from './components/StatTile.jsx'
import VersionChart from './components/VersionChart.jsx'
import Login from './components/Login.jsx'
import Deploys from './components/Deploys.jsx'
import Alerting from './components/Alerting.jsx'
import AISettings from './components/AISettings.jsx'
import Home from './components/Home.jsx'
import Explore from './components/Explore.jsx'
import { fetchApps, fetchVersions, me, logout } from './api.js'

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

const PAGE_META = {
  home: { title: 'Home', sub: 'what wardn can do, and how your apps are doing right now' },
  dashboards: { title: 'Latency by version', sub: 'latency percentiles across every deploy' },
  deploys: { title: 'Deploys', sub: 'deploy markers and before/after analysis' },
  alerting: { title: 'Alerting', sub: 'regression alerts and delivery channels' },
  ai: { title: 'AI Settings', sub: 'provider credentials and automatic root-cause analysis' },
  explore: { title: 'Explore', sub: 'query the raw samples behind any version' },
}

export default function App() {
  const [user, setUser] = useState(null)
  const [checking, setChecking] = useState(true)
  const [page, setPage] = useState('home')

  const [apps, setApps] = useState([])
  const [app, setApp] = useState('')
  const [versions, setVersions] = useState([])
  const [selected, setSelected] = useState('')
  const [range, setRange] = useState('1d')
  const [error, setError] = useState('')

  const selectedRef = useRef('')
  useEffect(() => {
    selectedRef.current = selected
  }, [selected])

  // On mount, ask the backend who we are. 401 -> show the login screen.
  useEffect(() => {
    me()
      .then((u) => setUser(u))
      .catch(() => setUser(null))
      .finally(() => setChecking(false))
  }, [])

  // A 401 from any data call means the session lapsed — bounce to login.
  const onAuthError = (e) => {
    if (String(e).includes('401')) setUser(null)
    else setError(String(e))
  }

  // Load apps once signed in. Exposed so AI Settings can refresh the list
  // after toggling a per-app setting.
  const loadApps = () =>
    fetchApps()
      .then((list) => {
        setApps(list)
        setApp((cur) => (cur || (list.length ? list[0].name : '')))
      })
      .catch(onAuthError)

  useEffect(() => {
    if (!user) return
    loadApps()
  }, [user])

  // Poll per-version stats for the selected app + range (dashboards page only).
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

  const selIdx = useMemo(() => versions.findIndex((v) => v.version === selected), [versions, selected])
  const selVer = selIdx >= 0 ? versions[selIdx] : null
  const prevVer = selIdx > 0 ? versions[selIdx - 1] : null

  async function handleLogout() {
    await logout()
    setUser(null)
    setApps([])
    setApp('')
    setVersions([])
    setSelected('')
    selectedRef.current = ''
    setError('')
  }

  // --- gate ---
  if (checking) return <div className="login-wrap" />
  if (!user) return <Login onSuccess={setUser} />

  const meta = PAGE_META[page] || PAGE_META.dashboards
  const sub = page === 'dashboards' ? `${meta.sub}${app ? ` · ${app}` : ''}` : meta.sub

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
            {page !== 'home' && (
              <>
                <span className="sep">/</span>
                <span className="here">{meta.title}</span>
              </>
            )}
          </div>
          <div className="header-main">
            <div className="header-title">
              <h1>{meta.title}</h1>
              <span className="header-sub">{sub}</span>
            </div>
            <div className="header-controls">
              {page !== 'ai' && page !== 'home' && (
                <select className="pill" value={app} onChange={(e) => setApp(e.target.value)} aria-label="Select app">
                  {apps.length === 0 && <option value="">no apps</option>}
                  {apps.map((a) => (
                    <option key={a.id} value={a.name}>
                      {a.name}
                    </option>
                  ))}
                </select>
              )}
              {(page === 'dashboards' || page === 'explore') && (
                <>
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
                </>
              )}
            </div>
          </div>
        </header>

        <div className="content">
          {page === 'home' && <Home apps={apps} onNavigate={setPage} onAuthError={onAuthError} />}
          {page === 'deploys' && <Deploys app={app} onAuthError={onAuthError} />}
          {page === 'alerting' && <Alerting apps={apps} appName={app} onAuthError={onAuthError} />}
          {page === 'ai' && <AISettings apps={apps} onAppsChanged={loadApps} onAuthError={onAuthError} />}
          {page === 'explore' && <Explore app={app} range={range} onAuthError={onAuthError} />}
          {page === 'dashboards' && (
            <div className="content-inner fade-in">
              <div className="section-label">
                SELECTED VERSION · <span style={{ color: 'var(--accent)' }}>{selected || '—'}</span>
                <span className="section-hint">click a point on any chart to inspect a version</span>
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
                  <VersionChart versions={versions} selected={selected} onSelect={setSelected} series="p99" />
                </div>
              </div>

              <div className="chart-row">
                <div className="panel">
                  <div className="panel-head">
                    <span className="panel-title">Latency by version · p95</span>
                    <span className="legend">
                      <span className="swatch" />
                      p95 per version
                    </span>
                  </div>
                  <div className="panel-body">
                    <VersionChart versions={versions} selected={selected} onSelect={setSelected} series="p95" />
                  </div>
                </div>

                <div className="panel">
                  <div className="panel-head">
                    <span className="panel-title">Latency by version · p90</span>
                    <span className="legend">
                      <span className="swatch" />
                      p90 per version
                    </span>
                  </div>
                  <div className="panel-body">
                    <VersionChart versions={versions} selected={selected} onSelect={setSelected} series="p90" />
                  </div>
                </div>
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
