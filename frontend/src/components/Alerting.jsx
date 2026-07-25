import { useEffect, useMemo, useState } from 'react'
import {
  createAlert,
  deleteAlert,
  fetchAlerts,
  fetchDeliveries,
  fetchMetricDefinitions,
  testAlert,
} from '../api.js'

const SUGGESTIONS = [
  {
    id: 'latency',
    metric_key: 'latency_p99',
    defaultPct: 25,
    unit: '%',
    title: 'Latency regression',
    blurb: 'Notify when p99 latency jumps after a deploy.',
  },
  {
    id: 'errors',
    metric_key: 'error_rate',
    defaultPct: 1,
    unit: 'pp',
    title: 'Error-rate spike',
    blurb: 'Notify when error rate rises by percentage points.',
  },
  {
    id: 'any',
    metric_key: '',
    defaultPct: 25,
    unit: '%',
    title: 'Any metric regression',
    blurb: 'Notify when any tracked metric crosses the threshold.',
  },
]

export default function Alerting({ apps, onAuthError }) {
  const [appId, setAppId] = useState('')
  const [metrics, setMetrics] = useState([])
  const [alerts, setAlerts] = useState([])
  const [deliveries, setDeliveries] = useState([])
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)

  const [channelType, setChannelType] = useState('slack')
  const [url, setUrl] = useState('')
  const [metricKey, setMetricKey] = useState('')
  const [thresholdPct, setThresholdPct] = useState('25')
  const [suggestedPct, setSuggestedPct] = useState(() =>
    Object.fromEntries(SUGGESTIONS.map((s) => [s.id, String(s.defaultPct)])),
  )

  const app = useMemo(
    () => apps.find((a) => String(a.id) === String(appId)),
    [apps, appId],
  )

  useEffect(() => {
    if (!apps.length) return
    setAppId((cur) => cur || String(apps[0].id))
  }, [apps])

  useEffect(() => {
    fetchMetricDefinitions()
      .then(setMetrics)
      .catch((e) => onAuthError?.(e))
  }, [])

  const reload = async () => {
    if (!app) return
    try {
      const [a, d] = await Promise.all([fetchAlerts(app.id), fetchDeliveries(app.id)])
      setAlerts(a)
      setDeliveries(d)
      setError('')
    } catch (e) {
      setError(String(e))
      onAuthError?.(e)
    }
  }

  useEffect(() => {
    reload()
  }, [app?.id])

  async function submitAlert({ metric_key, threshold_pct }) {
    if (!app) {
      setError('Select a service above')
      return
    }
    if (!url.trim()) {
      setError('Add a notification destination before creating an alert')
      return
    }
    setBusy(true)
    setNotice('')
    try {
      const channel_config =
        channelType === 'slack'
          ? { webhook_url: url.trim() }
          : { url: url.trim(), headers: {} }
      const mk = metric_key === '' || metric_key == null ? null : metric_key
      let thr = null
      if (threshold_pct !== '' && threshold_pct != null && !Number.isNaN(Number(threshold_pct))) {
        thr = Number(threshold_pct)
      }
      await createAlert(app.id, {
        channel_type: channelType,
        channel_config,
        metric_key: mk,
        threshold_pct: thr,
        on_verdict: 'regressed',
        enabled: true,
      })
      setError('')
      setNotice('Alert saved')
      await reload()
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy(false)
    }
  }

  async function onCreate(e) {
    e.preventDefault()
    await submitAlert({ metric_key: metricKey, threshold_pct: thresholdPct })
  }

  async function onAddSuggested(s) {
    await submitAlert({
      metric_key: s.metric_key,
      threshold_pct: suggestedPct[s.id],
    })
  }

  async function onTest(id) {
    setBusy(true)
    setNotice('')
    try {
      await testAlert(id)
      setNotice('Test notification sent')
      await reload()
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy(false)
    }
  }

  async function onDelete(id) {
    setBusy(true)
    try {
      await deleteAlert(id)
      setNotice('Alert removed')
      await reload()
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy(false)
    }
  }

  const metricLabel = (key) => {
    if (!key) return 'Any metric'
    const m = metrics.find((x) => x.key === key)
    return m ? m.name : key
  }

  const urlReady = Boolean(url.trim())

  return (
    <div className="content-inner fade-in alert-page">
      {/* Service context — page-owned, not header dropdown */}
      <div className="alert-toolbar">
        <div>
          <div className="section-label" style={{ margin: 0 }}>SERVICE</div>
          <div className="service-tabs" role="tablist" aria-label="Service">
            {apps.length === 0 && <span className="service-tab muted">No services</span>}
            {apps.map((a) => (
              <button
                key={a.id}
                type="button"
                role="tab"
                aria-selected={String(a.id) === String(appId)}
                className={`service-tab${String(a.id) === String(appId) ? ' on' : ''}`}
                onClick={() => setAppId(String(a.id))}
              >
                {a.name}
              </button>
            ))}
          </div>
        </div>
        {(notice || error) && (
          <div className={`alert-banner${error ? ' bad' : ''}`} role="status">
            {error || notice}
          </div>
        )}
      </div>

      {/* Destination first — shared by custom + suggested */}
      <section className="panel">
        <div className="panel-head">
          <span className="panel-title">Where to notify</span>
          <span className="legend">used by every alert you create on this page</span>
        </div>
        <div className="panel-body dest-grid">
          <label className="field">
            <span className="field-label">Channel</span>
            <div className="seg" role="group" aria-label="Channel type">
              <button
                type="button"
                className={`seg-btn${channelType === 'slack' ? ' on' : ''}`}
                onClick={() => setChannelType('slack')}
              >
                Slack
              </button>
              <button
                type="button"
                className={`seg-btn${channelType === 'webhook' ? ' on' : ''}`}
                onClick={() => setChannelType('webhook')}
              >
                Webhook
              </button>
            </div>
          </label>
          <label className="field grow">
            <span className="field-label">{channelType === 'slack' ? 'Incoming webhook URL' : 'Endpoint URL'}</span>
            <input
              className="text-input"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder={
                channelType === 'slack'
                  ? 'https://hooks.slack.com/services/…'
                  : 'https://example.com/hooks/wardn'
              }
            />
          </label>
          <div className={`dest-status${urlReady ? ' ok' : ''}`}>
            {urlReady ? 'Ready' : 'URL required'}
          </div>
        </div>
      </section>

      <div className="alert-split">
        {/* Custom create — vertical labeled fields */}
        <section className="panel">
          <div className="panel-head">
            <span className="panel-title">Custom alert</span>
          </div>
          <div className="panel-body">
            <form className="custom-form" onSubmit={onCreate}>
              <label className="field">
                <span className="field-label">Metric</span>
                <select
                  className="field-control"
                  value={metricKey}
                  onChange={(e) => setMetricKey(e.target.value)}
                >
                  <option value="">Any degraded metric</option>
                  {metrics.map((m) => (
                    <option key={m.key} value={m.key}>
                      {m.name}
                    </option>
                  ))}
                </select>
              </label>

              <label className="field">
                <span className="field-label">Threshold</span>
                <div className="threshold-row">
                  <span className="threshold-prefix">rises more than</span>
                  <input
                    className="threshold-input"
                    type="number"
                    min="0"
                    step="0.1"
                    value={thresholdPct}
                    onChange={(e) => setThresholdPct(e.target.value)}
                  />
                  <span className="threshold-suffix">%</span>
                </div>
                <span className="field-hint">Compared against the before/after deploy window</span>
              </label>

              <label className="field">
                <span className="field-label">Trigger</span>
                <div className="trigger-pill">Deploy marked regressed</div>
              </label>

              <button className="btn-primary" type="submit" disabled={busy || !app || !urlReady}>
                Create alert
              </button>
            </form>
          </div>
        </section>

        {/* Suggested — real cards */}
        <section className="panel">
          <div className="panel-head">
            <span className="panel-title">Suggested</span>
            <span className="legend">one click after the URL is set</span>
          </div>
          <div className="panel-body suggest-grid">
            {SUGGESTIONS.map((s) => (
              <article key={s.id} className="suggest-card">
                <header className="suggest-card-head">
                  <h3>{s.title}</h3>
                  <span className="suggest-tag">{metricLabel(s.metric_key)}</span>
                </header>
                <p className="suggest-blurb">{s.blurb}</p>
                <div className="suggest-footer">
                  <label className="threshold-inline">
                    <span>Threshold</span>
                    <input
                      className="threshold-input sm"
                      type="number"
                      min="0"
                      step="0.1"
                      value={suggestedPct[s.id]}
                      onChange={(e) =>
                        setSuggestedPct((prev) => ({ ...prev, [s.id]: e.target.value }))
                      }
                    />
                    <span>{s.unit}</span>
                  </label>
                  <button
                    type="button"
                    className="btn-secondary"
                    disabled={busy || !app || !urlReady}
                    onClick={() => onAddSuggested(s)}
                  >
                    Add
                  </button>
                </div>
              </article>
            ))}
          </div>
        </section>
      </div>

      <section className="panel">
        <div className="panel-head">
          <span className="panel-title">Configured{app ? ` · ${app.name}` : ''}</span>
          <span className="legend">{alerts.length} active</span>
        </div>
        <div className="panel-body" style={{ padding: alerts.length ? 0 : undefined }}>
          {alerts.length === 0 ? (
            <div className="empty-block">
              <strong>No alerts for this service</strong>
              <span>Create one on the left, or add a suggested alert.</span>
            </div>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th>Channel</th>
                  <th>Metric</th>
                  <th>Threshold</th>
                  <th>Trigger</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {alerts.map((a) => (
                  <tr key={a.id}>
                    <td>
                      <span className="chip">{a.channel_type}</span>
                    </td>
                    <td>{metricLabel(a.metric_key)}</td>
                    <td className="mono">
                      {a.threshold_pct != null ? `${a.threshold_pct}${a.metric_key === 'error_rate' ? ' pp' : '%'}` : '—'}
                    </td>
                    <td>{a.on_verdict}</td>
                    <td className="row-actions">
                      <button type="button" className="ghost-btn" disabled={busy} onClick={() => onTest(a.id)}>
                        Test
                      </button>
                      <button type="button" className="ghost-btn danger" disabled={busy} onClick={() => onDelete(a.id)}>
                        Remove
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>

      <section className="panel">
        <div className="panel-head">
          <span className="panel-title">Delivery history{app ? ` · ${app.name}` : ''}</span>
        </div>
        <div className="panel-body" style={{ padding: deliveries.length ? 0 : undefined }}>
          {deliveries.length === 0 ? (
            <div className="empty-block">
              <strong>No deliveries yet</strong>
              <span>They appear when a regression fires, or when you send a test.</span>
            </div>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th>Alert</th>
                  <th>Deploy</th>
                  <th>Status</th>
                  <th>Code</th>
                  <th>When</th>
                </tr>
              </thead>
              <tbody>
                {deliveries.map((d) => (
                  <tr key={d.id}>
                    <td>#{d.alert_config_id}</td>
                    <td>{d.deploy_event_id ? `#${d.deploy_event_id}` : '—'}</td>
                    <td>
                      <span className={`badge status-${d.status === 'sent' ? 'healthy' : 'failed'}`}>{d.status}</span>
                    </td>
                    <td>{d.response_code ?? '—'}</td>
                    <td className="muted">{new Date(d.created_at).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>
    </div>
  )
}
