import { useEffect, useMemo, useState } from 'react'
import { createAlert, deleteAlert, fetchAlerts, fetchDeliveries, testAlert } from '../api.js'

export default function Alerting({ apps, appName, onAuthError }) {
  const app = useMemo(() => apps.find((a) => a.name === appName), [apps, appName])
  const [alerts, setAlerts] = useState([])
  const [deliveries, setDeliveries] = useState([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const [channelType, setChannelType] = useState('slack')
  const [url, setUrl] = useState('')
  const [metricKey, setMetricKey] = useState('')

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

  async function onCreate(e) {
    e.preventDefault()
    if (!app || !url.trim()) return
    setBusy(true)
    try {
      const channel_config =
        channelType === 'slack'
          ? { webhook_url: url.trim() }
          : { url: url.trim(), headers: {} }
      await createAlert(app.id, {
        channel_type: channelType,
        channel_config,
        metric_key: metricKey.trim() || null,
        on_verdict: 'regressed',
        enabled: true,
      })
      setUrl('')
      setMetricKey('')
      await reload()
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy(false)
    }
  }

  async function onTest(id) {
    setBusy(true)
    try {
      await testAlert(id)
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
      await reload()
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="content-inner fade-in">
      <div className="section-label">ALERTING · {appName || '—'}</div>

      <div className="panel">
        <div className="panel-head">
          <span className="panel-title">New alert channel</span>
        </div>
        <div className="panel-body">
          <form className="alert-form" onSubmit={onCreate}>
            <label>
              Channel
              <select className="pill" value={channelType} onChange={(e) => setChannelType(e.target.value)}>
                <option value="slack">Slack webhook</option>
                <option value="webhook">Generic webhook</option>
              </select>
            </label>
            <label>
              {channelType === 'slack' ? 'Slack webhook URL' : 'Webhook URL'}
              <input
                className="text-input"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://hooks.slack.com/services/…"
                required
              />
            </label>
            <label>
              Metric filter (optional)
              <input
                className="text-input"
                value={metricKey}
                onChange={(e) => setMetricKey(e.target.value)}
                placeholder="latency_p99 or empty = any"
              />
            </label>
            <button className="login-btn" type="submit" disabled={busy || !app} style={{ width: 'auto', padding: '10px 18px' }}>
              Add alert
            </button>
          </form>
        </div>
      </div>

      <div className="panel">
        <div className="panel-head">
          <span className="panel-title">Configured alerts</span>
        </div>
        <div className="panel-body" style={{ padding: 0 }}>
          {alerts.length === 0 ? (
            <div className="empty" style={{ height: 120 }}>
              No alerts yet
            </div>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th>Channel</th>
                  <th>Metric</th>
                  <th>On</th>
                  <th>Enabled</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {alerts.map((a) => (
                  <tr key={a.id}>
                    <td>{a.channel_type}</td>
                    <td className="mono">{a.metric_key || 'any'}</td>
                    <td>{a.on_verdict}</td>
                    <td>{a.enabled ? 'yes' : 'no'}</td>
                    <td className="row-actions">
                      <button type="button" className="ghost-btn" disabled={busy} onClick={() => onTest(a.id)}>
                        Send test
                      </button>
                      <button type="button" className="ghost-btn danger" disabled={busy} onClick={() => onDelete(a.id)}>
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      <div className="panel">
        <div className="panel-head">
          <span className="panel-title">Delivery history</span>
        </div>
        <div className="panel-body" style={{ padding: 0 }}>
          {deliveries.length === 0 ? (
            <div className="empty" style={{ height: 100 }}>
              No deliveries yet
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
                    <td>#{d.deploy_event_id}</td>
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
      </div>

      {error && (
        <div className="empty" style={{ height: 'auto', color: 'var(--danger)' }}>
          {error}
        </div>
      )}
    </div>
  )
}
