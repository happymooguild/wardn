import { useEffect, useMemo, useState } from 'react'

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

  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Suggest the metrics wardn already knows about (built-in sources + demo set).
  const suggestions = useMemo(() => {
    const s = new Set(['wardn_demo_latency_ms', 'wardn_demo_error_rate', 'wardn_demo_rps'])
    for (const d of existing || []) if (d.signoz_metric) s.add(d.signoz_metric)
    return [...s]
  }, [existing])

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

              <label className="field-label">SigNoz metric</label>
              <input className="text-input" placeholder="e.g. wardn_demo_rps" value={metric} list="wardn-metric-suggestions"
                onChange={(e) => setMetric(e.target.value)} spellCheck={false} />
              <datalist id="wardn-metric-suggestions">
                {suggestions.map((m) => <option key={m} value={m} />)}
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
                  : 'No recent versions had this metric yet — it will populate on the next deploy.'}
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
