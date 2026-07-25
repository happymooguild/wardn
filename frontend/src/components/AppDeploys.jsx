import { useEffect, useState } from 'react'
import { fetchDeploys, fetchDeploy } from '../api.js'
import AskAI from './AskAI.jsx'

const POLL_MS = 5000

// Drill-in view for a single service: its deploy markers on the left, the
// selected marker's before/after analysis on the right. Rendered when a service
// is opened from the Deploys landing list.
export default function AppDeploys({ appName, onBack, onAuthError }) {
  const [deploys, setDeploys] = useState([])
  const [selectedId, setSelectedId] = useState(null)
  const [detail, setDetail] = useState(null)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!appName) return
    let alive = true
    const load = async () => {
      try {
        const list = await fetchDeploys(appName)
        if (!alive) return
        setDeploys(list)
        setError('')
        setSelectedId((cur) => cur ?? (list.length ? list[0].id : null))
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
  }, [appName])

  useEffect(() => {
    if (!selectedId) {
      setDetail(null)
      return
    }
    let alive = true
    const load = async () => {
      try {
        const d = await fetchDeploy(selectedId)
        if (alive) setDetail(d)
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
  }, [selectedId])

  return (
    <div className="content-inner fade-in">
      <div className="section-label" style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <button type="button" className="back-link" onClick={onBack}>
          ← All services
        </button>
        <span className="sep">/</span>
        <span>{appName}</span>
      </div>

      <div className="split">
        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">Recent deploys</span>
          </div>
          <div className="panel-body" style={{ padding: 0 }}>
            {deploys.length === 0 ? (
              <div className="empty" style={{ height: 160 }}>
                No deploy markers yet — POST /api/v1/deployments
              </div>
            ) : (
              <table className="data-table">
                <thead>
                  <tr>
                    <th>Version</th>
                    <th>Status</th>
                    <th>Source</th>
                    <th>When</th>
                  </tr>
                </thead>
                <tbody>
                  {deploys.map((d) => (
                    <tr
                      key={d.id}
                      className={selectedId === d.id ? 'on' : ''}
                      onClick={() => setSelectedId(d.id)}
                    >
                      <td className="mono">{d.version}</td>
                      <td>
                        <span className={`badge status-${d.status}`}>{d.status}</span>
                      </td>
                      <td>{d.source}</td>
                      <td className="muted">{formatTime(d.deployed_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>

        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">
              Detail ·{' '}
              <span style={{ color: 'var(--accent)', fontFamily: 'var(--font-mono)' }}>
                {detail?.deploy?.version || '—'}
              </span>
            </span>
          </div>
          <div className="panel-body">
            {!detail ? (
              <div className="empty" style={{ height: 160 }}>
                Select a deploy
              </div>
            ) : (
              <>
                <div className="meta-grid">
                  <Meta label="Previous" value={detail.deploy.previous_version || 'baseline'} />
                  <Meta label="Environment" value={detail.deploy.environment} />
                  <Meta label="Status" value={detail.deploy.status} />
                  <Meta label="Failure" value={detail.deploy.failure_reason || '—'} />
                </div>
                <div className="section-label" style={{ marginTop: 18 }}>
                  BEFORE / AFTER SNAPSHOTS
                </div>
                {(detail.snapshots || []).length === 0 ? (
                  <div className="muted" style={{ marginTop: 8, fontSize: 13 }}>
                    Snapshots appear after the analyzer finishes the after-window.
                  </div>
                ) : (
                  <div className="snap-grid">
                    {detail.snapshots.map((s) => (
                      <div key={s.metric_key} className={`snap-card${s.degraded ? ' bad' : ''}`}>
                        <div className="snap-key">{s.metric_key}</div>
                        <div className="snap-row">
                          <span>before</span>
                          <b>{fmtNum(s.before_value)}</b>
                        </div>
                        <div className="snap-row">
                          <span>after</span>
                          <b>{fmtNum(s.after_value)}</b>
                        </div>
                        <div className="snap-row">
                          <span>delta</span>
                          <b>{s.delta_pct != null ? `${s.delta_pct.toFixed(1)}%` : '—'}</b>
                        </div>
                      </div>
                    ))}
                  </div>
                )}

                <AskAI
                  deployId={detail.deploy.id}
                  initial={detail.analysis}
                  onAuthError={onAuthError}
                />
              </>
            )}
          </div>
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

function Meta({ label, value }) {
  return (
    <div className="meta">
      <span>{label}</span>
      <b>{value}</b>
    </div>
  )
}

function formatTime(iso) {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

function fmtNum(v) {
  if (v == null) return '—'
  return typeof v === 'number' ? v.toFixed(2) : String(v)
}
