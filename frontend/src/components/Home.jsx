// Landing page: what wardn does, a live cross-app pulse, and shortcuts into
// every other section — the front door Dashboards/Deploys/Alerting didn't have.
import { useEffect, useState } from 'react'
import { fetchDeploys } from '../api.js'
import { Icon } from './Sidebar.jsx'

const POLL_MS = 10000

const FEATURES = [
  {
    icon: 'grid',
    page: 'dashboards',
    title: 'Dashboards',
    desc: 'Latency percentiles by version, with a time-range selector and drill-down into any release.',
  },
  {
    icon: 'deploys',
    page: 'deploys',
    title: 'Deploys',
    desc: 'Every deploy marker with its before/after snapshot — see exactly what a release changed.',
  },
  {
    icon: 'alert',
    page: 'alerting',
    title: 'Alerting',
    desc: 'Slack or webhook notifications the moment a deploy regresses, plus delivery history.',
  },
  {
    icon: 'ai',
    page: 'ai',
    title: 'AI root cause',
    desc: 'Ask AI reasons over before/after metrics plus a bounded sample of logs and traces.',
  },
  {
    icon: 'explore',
    page: 'explore',
    title: 'Explore',
    desc: 'Query the raw samples behind any version — the drill-down below the percentile charts.',
  },
]

export default function Home({ apps, onNavigate, onAuthError }) {
  const [deploysByApp, setDeploysByApp] = useState({})
  const [error, setError] = useState('')

  useEffect(() => {
    if (apps.length === 0) return
    let alive = true
    const load = async () => {
      try {
        const entries = await Promise.all(
          apps.map(async (a) => [a.name, await fetchDeploys(a.name)])
        )
        if (!alive) return
        setDeploysByApp(Object.fromEntries(entries))
        setError('')
      } catch (e) {
        if (alive) {
          setError(String(e))
          onAuthError?.(e)
        }
      }
    }
    load()
    const id = setInterval(load, POLL_MS)
    return () => {
      alive = false
      clearInterval(id)
    }
  }, [apps])

  const allDeploys = Object.entries(deploysByApp).flatMap(([appName, list]) =>
    list.map((d) => ({ ...d, appName }))
  )
  const recent = [...allDeploys]
    .sort((a, b) => new Date(b.deployed_at) - new Date(a.deployed_at))
    .slice(0, 6)
  const regressed = allDeploys.filter((d) => d.status === 'regressed').length
  const healthy = allDeploys.filter((d) => d.status === 'healthy').length

  return (
    <div className="content-inner fade-in">
      <div className="hero panel">
        <div className="hero-kicker">WARDN · DEPLOY-AWARE OBSERVABILITY</div>
        <h2 className="hero-title">Did that deploy make things worse?</h2>
        <p className="hero-sub">
          wardn detects when a new version goes live, compares metrics before/after, and can alert
          Slack or a webhook when things regress — with AI root-cause reasoning one click away.
        </p>
      </div>

      <div className="tiles">
        <StatBox label="APPS MONITORED" value={apps.length} />
        <StatBox label="DEPLOYS TRACKED" value={allDeploys.length} />
        <StatBox label="HEALTHY" value={healthy} />
        <StatBox label="REGRESSED" value={regressed} />
      </div>

      <div className="section-label">WHAT YOU CAN DO</div>
      <div className="cards">
        {FEATURES.map((f) => (
          <button key={f.page} className="card" type="button" onClick={() => onNavigate?.(f.page)}>
            <div className="card-icon">{Icon[f.icon]}</div>
            <div className="card-title">{f.title}</div>
            <div className="card-desc">{f.desc}</div>
            <div className="card-arrow">Open →</div>
          </button>
        ))}
      </div>

      <div className="panel">
        <div className="panel-head">
          <span className="panel-title">Recent activity</span>
        </div>
        <div className="panel-body" style={{ padding: 0 }}>
          {recent.length === 0 ? (
            <div className="empty" style={{ height: 140 }}>
              No deploy markers yet — POST /api/v1/deployments
            </div>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th>App</th>
                  <th>Version</th>
                  <th>Status</th>
                  <th>When</th>
                </tr>
              </thead>
              <tbody>
                {recent.map((d) => (
                  <tr key={`${d.appName}-${d.id}`} onClick={() => onNavigate?.('deploys')}>
                    <td>{d.appName}</td>
                    <td className="mono">{d.version}</td>
                    <td>
                      <span className={`badge status-${d.status}`}>{d.status}</span>
                    </td>
                    <td className="muted">{new Date(d.deployed_at).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {error && (
        <div className="empty" style={{ height: 'auto', color: 'var(--danger)' }}>
          {error}
        </div>
      )}
    </div>
  )
}

function StatBox({ label, value }) {
  return (
    <div className="tile">
      <div className="label">{label}</div>
      <div className="value-row">
        <span className="value">{value}</span>
      </div>
    </div>
  )
}
