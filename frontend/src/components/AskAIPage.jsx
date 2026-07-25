import { useEffect, useState } from 'react'
import { fetchAppVersions, compareVersions, rootCauseVersions } from '../api.js'

// Ask AI: pick a service, choose two versions, and let the model summarize the
// difference across metrics + logs + traces. If it's a regression, a second pass
// digs into logs/traces for the root cause.
export default function AskAIPage({ apps, onAuthError }) {
  const [app, setApp] = useState(null)
  const [versions, setVersions] = useState([])
  const [vA, setVA] = useState('')
  const [vB, setVB] = useState('')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [result, setResult] = useState(null)
  const [rootCause, setRootCause] = useState(null)

  useEffect(() => {
    if (!app) return
    setResult(null)
    setRootCause(null)
    setError('')
    fetchAppVersions(app.id)
      .then((vs) => {
        setVersions(vs)
        setVB(vs[0] || '') // newest
        setVA(vs[1] || vs[0] || '') // previous
      })
      .catch((e) => {
        setError(String(e.message || e))
        onAuthError?.(e)
      })
  }, [app])

  async function runCompare() {
    setBusy('compare')
    setError('')
    setRootCause(null)
    try {
      setResult(await compareVersions(app.id, vA, vB))
    } catch (e) {
      setError(String(e.message || e))
    } finally {
      setBusy('')
    }
  }

  async function runRootCause() {
    setBusy('rc')
    setError('')
    try {
      setRootCause(await rootCauseVersions(app.id, vA, vB))
    } catch (e) {
      setError(String(e.message || e))
    } finally {
      setBusy('')
    }
  }

  if (!app) {
    return (
      <div className="content-inner fade-in">
        <div className="section-label">ASK AI · pick a service</div>
        {apps.length === 0 ? (
          <div className="empty" style={{ height: 160 }}>No services registered.</div>
        ) : (
          <div className="dash-list">
            {apps.map((a) => (
              <div key={a.id} className="dash-list-item" role="button" tabIndex={0} onClick={() => setApp(a)}>
                <span className="dash-list-name">{a.name}</span>
                <span className="muted">compare versions →</span>
              </div>
            ))}
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="content-inner fade-in">
      <div className="section-label dash-view-head">
        <span className="dash-view-title">
          <button type="button" className="back-link" onClick={() => setApp(null)}>← All services</button>
          <span className="sep">/</span>
          <span>{app.name}</span>
        </span>
      </div>

      <div className="panel">
        <div className="panel-body">
          {versions.length < 1 ? (
            <div className="empty" style={{ height: 120 }}>No versions yet — deploy this service to compare.</div>
          ) : (
            <div className="compare-controls">
              <label className="field-label">
                Baseline (A)
                <select className="text-input" value={vA} onChange={(e) => setVA(e.target.value)}>
                  {versions.map((v) => <option key={v} value={v}>{v}</option>)}
                </select>
              </label>
              <span className="compare-vs">→</span>
              <label className="field-label">
                Newer (B)
                <select className="text-input" value={vB} onChange={(e) => setVB(e.target.value)}>
                  {versions.map((v) => <option key={v} value={v}>{v}</option>)}
                </select>
              </label>
              <button
                type="button"
                className="login-btn"
                style={{ width: 'auto', padding: '10px 18px' }}
                disabled={busy === 'compare' || !vA || !vB || vA === vB}
                onClick={runCompare}
              >
                {busy === 'compare' ? 'Analyzing…' : 'Summarize differences'}
              </button>
            </div>
          )}
          {error && <div className="ai-error" style={{ marginTop: 12 }}>{error}</div>}
        </div>
      </div>

      {result && (
        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{result.version_a} → {result.version_b}</span>
            <span className={`badge ${result.is_regression ? 'status-regressed' : 'status-healthy'}`}>
              {result.is_regression ? 'regression' : 'no regression'}
            </span>
          </div>
          <div className="panel-body">
            <div className="ai-summary">{result.summary}</div>
            {result.detail && <p className="ai-detail">{result.detail}</p>}

            <table className="data-table" style={{ marginTop: 12 }}>
              <thead>
                <tr><th>Metric</th><th>{result.version_a}</th><th>{result.version_b}</th><th>Δ</th></tr>
              </thead>
              <tbody>
                {(result.metrics || []).map((m) => (
                  <tr key={m.key}>
                    <td>{m.metric}</td>
                    <td className="mono">{fmt(m.a, m.unit)}</td>
                    <td className="mono">{fmt(m.b, m.unit)}</td>
                    <td className={m.degraded ? 'delta-bad' : 'muted'}>
                      {m.delta_pct != null ? `${m.delta_pct > 0 ? '+' : ''}${m.delta_pct.toFixed(1)}%` : '—'}
                      {m.degraded ? ' ▲' : ''}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>

            {result.evidence?.length > 0 && (
              <>
                <div className="section-label" style={{ marginTop: 16 }}>EVIDENCE</div>
                <ul className="ai-evidence">{result.evidence.map((e, i) => <li key={i}>{e}</li>)}</ul>
              </>
            )}
            <div className="muted" style={{ fontSize: 12, marginTop: 10 }}>{result.provider} · {result.model}</div>

            {result.is_regression && (
              <div style={{ marginTop: 16 }}>
                <button
                  type="button"
                  className="login-btn"
                  style={{ width: 'auto', padding: '10px 18px' }}
                  disabled={busy === 'rc'}
                  onClick={runRootCause}
                >
                  {busy === 'rc' ? 'Investigating…' : 'Find root cause'}
                </button>
              </div>
            )}
          </div>
        </div>
      )}

      {rootCause && (
        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">Root cause</span>
            <span className="badge status-inconclusive">{rootCause.confidence} confidence</span>
          </div>
          <div className="panel-body">
            <div className="ai-summary">{rootCause.cause}</div>
            {rootCause.evidence?.length > 0 && (
              <>
                <div className="section-label" style={{ marginTop: 12 }}>EVIDENCE</div>
                <ul className="ai-evidence">{rootCause.evidence.map((e, i) => <li key={i}>{e}</li>)}</ul>
              </>
            )}
            {rootCause.suggested?.length > 0 && (
              <>
                <div className="section-label" style={{ marginTop: 12 }}>SUGGESTED NEXT STEPS</div>
                <ul className="ai-evidence">{rootCause.suggested.map((e, i) => <li key={i}>{e}</li>)}</ul>
              </>
            )}
            <div className="muted" style={{ fontSize: 12, marginTop: 10 }}>{rootCause.provider} · {rootCause.model}</div>
          </div>
        </div>
      )}
    </div>
  )
}

function fmt(v, unit) {
  return v == null ? '—' : `${v.toFixed(1)}${unit || ''}`
}
