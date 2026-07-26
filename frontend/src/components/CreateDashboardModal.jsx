import { useEffect, useMemo, useState } from 'react'
import { fetchAvailableMetrics } from '../api.js'

// Create a custom dashboard: name it, point it at a SigNoz metric, pick how to
// display it. On create the backend backfills recent deploys so it isn't empty.
export default function CreateDashboardModal({ existing, onClose, onCreate, onCreated }) {
  const [name, setName] = useState('')
  const [metric, setMetric] = useState('')
  const [kind, setKind] = useState('single')
  const [unit, setUnit] = useState('')
  const [decimals, setDecimals] = useState(0)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [done, setDone] = useState(null) // { dashboard, backfilled_versions }
  const [available, setAvailable] = useState([]) // [{name,type,unit}]

  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Discover metrics from SigNoz; fall back silently to just the known set.
  useEffect(() => {
    fetchAvailableMetrics().then(setAvailable).catch(() => setAvailable([]))
  }, [])

  // Metric options: discovered ∪ ones already used by existing dashboards.
  const options = useMemo(() => {
    const byName = new Map()
    for (const m of available) if (m.name) byName.set(m.name, m)
    for (const d of existing || []) if (d.signoz_metric && !byName.has(d.signoz_metric)) byName.set(d.signoz_metric, { name: d.signoz_metric, unit: d.unit })
    return [...byName.values()].sort((a, b) => a.name.localeCompare(b.name))
  }, [available, existing])

  // When a known metric is picked, prefill a sensible unit.
  function pickMetric(v) {
    setMetric(v)
    const m = options.find((o) => o.name === v)
    if (m && m.unit && (!unit || unit === '')) {
      setUnit(m.unit === '1' ? '' : m.unit)
    }
  }

  async function submit(e) {
    e.preventDefault()
    setErr('')
    setBusy(true)
    try {
      const res = await onCreate({ name: name.trim(), signoz_metric: metric.trim(), kind, unit, decimals: Number(decimals) })
      setDone(res)
      onCreated?.()
    } catch (e2) {
      setErr(String(e2.message || e2))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="modal-overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal" role="dialog" aria-modal="true">
        {!done ? (
          <>
            <div className="modal-head">
              <div className="modal-title">Create custom dashboard</div>
              <div className="modal-sub">
                Point it at a SigNoz metric. wardn snapshots it per version around every deploy and
                backfills recent deploys now.
              </div>
            </div>
            <form className="modal-body" onSubmit={submit}>
              <label className="field-label">Name</label>
              <input className="text-input" autoFocus placeholder="e.g. Request rate" value={name}
                onChange={(e) => setName(e.target.value)} spellCheck={false} />

              <label className="field-label">
                SigNoz metric {options.length > 0 && <span className="field-hint">· {options.length} discovered</span>}
              </label>
              <input className="text-input" placeholder="type or pick a metric…" value={metric} list="wardn-metric-suggestions"
                onChange={(e) => pickMetric(e.target.value)} spellCheck={false} />
              <datalist id="wardn-metric-suggestions">
                {options.map((m) => <option key={m.name} value={m.name}>{m.type ? `${m.name} · ${m.type}` : m.name}</option>)}
              </datalist>

              <div className="field-row">
                <div style={{ flex: 1 }}>
                  <label className="field-label">Display</label>
                  <select className="text-input" value={kind} onChange={(e) => setKind(e.target.value)}>
                    <option value="single">Single value per version</option>
                    <option value="percentiles">Percentiles (p99 / p95 / p90)</option>
                  </select>
                </div>
                <div style={{ width: 96 }}>
                  <label className="field-label">Unit</label>
                  <input className="text-input" placeholder="ms, %…" value={unit} onChange={(e) => setUnit(e.target.value)} />
                </div>
                <div style={{ width: 84 }}>
                  <label className="field-label">Decimals</label>
                  <input className="text-input" type="number" min="0" max="4" value={decimals}
                    onChange={(e) => setDecimals(e.target.value)} />
                </div>
              </div>

              {err && <div className="modal-err">{err}</div>}
              <div className="modal-actions">
                <button type="button" className="ghost-btn" onClick={onClose} disabled={busy}>Cancel</button>
                <button type="submit" className="login-btn" style={{ width: 'auto', padding: '10px 18px' }}
                  disabled={busy || !name.trim() || !metric.trim()}>
                  {busy ? 'Creating…' : 'Create'}
                </button>
              </div>
            </form>
          </>
        ) : (
          <>
            <div className="modal-head">
              <div className="modal-title">“{done.dashboard.name}” created</div>
              <div className="modal-sub">
                {done.backfilled_versions > 0
                  ? `Backfilled ${done.backfilled_versions} recent version(s) from SigNoz. Future deploys populate automatically.`
                  : 'No recent versions had this metric yet - it will populate on the next deploy.'}
              </div>
            </div>
            <div className="modal-body">
              <div className="modal-actions">
                <button type="button" className="login-btn" style={{ width: 'auto', padding: '10px 18px' }} onClick={onClose}>
                  Done
                </button>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
