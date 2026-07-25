// Ad-hoc metric browser: pick a metric + version for the app/range chosen in
// the header, and see the raw samples behind that version's percentiles —
// the drill-down Dashboards doesn't offer (it only plots one point/version).
import { useEffect, useMemo, useState } from 'react'
import { fetchVersions, fetchVersionSeries } from '../api.js'
import SeriesChart from './SeriesChart.jsx'

const POLL_MS = 5000

export default function Explore({ app, range, onAuthError }) {
  const [metric, setMetric] = useState('latency_ms')
  const [metricInput, setMetricInput] = useState('latency_ms')
  const [versions, setVersions] = useState([])
  const [version, setVersion] = useState('')
  const [points, setPoints] = useState([])
  const [error, setError] = useState('')

  useEffect(() => {
    if (!app) return
    let alive = true
    const load = async () => {
      try {
        const vs = await fetchVersions(app, range, metric)
        if (!alive) return
        setVersions(vs)
        setError('')
        setVersion((cur) => (cur && vs.some((v) => v.version === cur) ? cur : vs[vs.length - 1]?.version || ''))
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
  }, [app, range, metric])

  useEffect(() => {
    if (!app || !version) {
      setPoints([])
      return
    }
    let alive = true
    const load = async () => {
      try {
        const pts = await fetchVersionSeries(app, version, range, metric)
        if (alive) setPoints(pts)
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
  }, [app, version, range, metric])

  const stats = useMemo(() => {
    if (points.length === 0) return null
    const vals = points.map((p) => p.value)
    const sum = vals.reduce((a, b) => a + b, 0)
    return {
      count: points.length,
      avg: sum / vals.length,
      min: Math.min(...vals),
      max: Math.max(...vals),
    }
  }, [points])

  function onMetricSubmit(e) {
    e.preventDefault()
    const next = metricInput.trim() || 'latency_ms'
    setMetricInput(next)
    setMetric(next)
    setVersion('')
  }

  return (
    <div className="content-inner fade-in">
      <div className="section-label">
        EXPLORE · {app || '—'}
        <span className="section-hint">query the raw samples behind any version</span>
      </div>

      <div className="panel">
        <div className="panel-body">
          <form className="alert-form" onSubmit={onMetricSubmit}>
            <label>
              Metric
              <input
                className="text-input"
                value={metricInput}
                onChange={(e) => setMetricInput(e.target.value)}
                placeholder="latency_ms"
              />
            </label>
            <label>
              Version
              <select className="pill" value={version} onChange={(e) => setVersion(e.target.value)}>
                {versions.length === 0 && <option value="">no versions</option>}
                {versions.map((v) => (
                  <option key={v.version} value={v.version}>
                    {v.version}
                  </option>
                ))}
              </select>
            </label>
            <button className="login-btn" type="submit" style={{ width: 'auto', padding: '10px 18px' }}>
              Run query
            </button>
          </form>
        </div>
      </div>

      {stats && (
        <div className="tiles">
          <StatBox label="SAMPLES" value={stats.count} />
          <StatBox label="AVG" value={`${stats.avg.toFixed(1)}`} />
          <StatBox label="MIN" value={`${stats.min.toFixed(1)}`} />
          <StatBox label="MAX" value={`${stats.max.toFixed(1)}`} />
        </div>
      )}

      <div className="panel">
        <div className="panel-head">
          <span className="panel-title">
            {metric} · <span style={{ color: 'var(--accent)', fontFamily: 'var(--font-mono)' }}>{version || '—'}</span>
          </span>
          <span className="legend">
            <span className="swatch" />
            raw samples in range
          </span>
        </div>
        <div className="panel-body">
          <SeriesChart points={points} />
        </div>
      </div>

      <div className="panel">
        <div className="panel-head">
          <span className="panel-title">Versions in range</span>
        </div>
        <div className="panel-body" style={{ padding: 0 }}>
          {versions.length === 0 ? (
            <div className="empty" style={{ height: 120 }}>
              no versions for this app/range yet
            </div>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th>Version</th>
                  <th>p50</th>
                  <th>p90</th>
                  <th>p95</th>
                  <th>p99</th>
                  <th>Samples</th>
                </tr>
              </thead>
              <tbody>
                {versions.map((v) => (
                  <tr key={v.version} className={version === v.version ? 'on' : ''} onClick={() => setVersion(v.version)}>
                    <td className="mono">{v.version}</td>
                    <td>{Math.round(v.p50)}</td>
                    <td>{Math.round(v.p90)}</td>
                    <td>{Math.round(v.p95)}</td>
                    <td>{Math.round(v.p99)}</td>
                    <td className="muted">{v.count}</td>
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
