// AI provider setup: plug in an API key, pick a model, verify it works, and
// choose which services get automatic analysis on a regression.
//
// The key is write-only over the API - the server returns only its last four
// characters, so this page can show which key is installed without ever
// receiving it back.
import { useEffect, useState } from 'react'
import {
  fetchAIProvider,
  saveAIProvider,
  deleteAIProvider,
  testAIProvider,
  setAppAIEnabled,
} from '../api.js'

export default function AISettings({ apps, onAppsChanged, onAuthError }) {
  const [state, setState] = useState(null)
  const [kind, setKind] = useState('anthropic')
  const [model, setModel] = useState('')
  const [customMode, setCustomMode] = useState(false)
  const [apiKey, setApiKey] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const load = async () => {
    try {
      const data = await fetchAIProvider()
      setState(data)
      if (data.provider?.kind) setKind(data.provider.kind)
      if (data.provider?.model) {
        setModel(data.provider.model)
        const opts = data.models?.[data.provider.kind] ?? []
        setCustomMode(!opts.includes(data.provider.model))
      }
      if (data.provider?.base_url) setBaseURL(data.provider.base_url)
    } catch (e) {
      setError(String(e.message || e))
      onAuthError?.(e)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const defaultModel = state?.default_models?.[kind] ?? ''
  const modelOptions = state?.models?.[kind] ?? []

  async function save(e) {
    e.preventDefault()
    setBusy('save')
    setError('')
    setNotice('')
    try {
      await saveAIProvider({ kind, model: model.trim(), api_key: apiKey.trim(), base_url: baseURL.trim() })
      setApiKey('')
      setNotice('Provider saved.')
      await load()
    } catch (err) {
      setError(String(err.message || err))
    } finally {
      setBusy('')
    }
  }

  async function test() {
    setBusy('test')
    setError('')
    setNotice('')
    try {
      const res = await testAIProvider()
      setNotice(`Connected to ${res.provider} · ${res.model} (from ${res.source}).`)
    } catch (err) {
      setError(String(err.message || err))
    } finally {
      setBusy('')
    }
  }

  async function remove() {
    setBusy('delete')
    setError('')
    setNotice('')
    try {
      await deleteAIProvider()
      setNotice('Provider removed.')
      await load()
    } catch (err) {
      setError(String(err.message || err))
    } finally {
      setBusy('')
    }
  }

  async function toggleApp(appId, enabled) {
    try {
      await setAppAIEnabled(appId, enabled)
      onAppsChanged?.()
    } catch (err) {
      setError(String(err.message || err))
    }
  }

  const envConfigured = state?.configured && state?.source === 'environment'

  return (
    <div className="content-inner fade-in">
      <div className="section-label">AI PROVIDER</div>

      <div className="split">
        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">Credentials</span>
            {state?.configured && (
              <span className={`badge ${state.source === 'database' ? 'status-healthy' : 'badge-env'}`}>
                {state.source === 'database' ? 'configured' : 'from environment'}
              </span>
            )}
          </div>
          <div className="panel-body">
            {envConfigured && (
              <div className="muted" style={{ fontSize: 13, marginBottom: 12 }}>
                A key is currently supplied by the environment
                (<span className="mono">ANTHROPIC_API_KEY</span> /{' '}
                <span className="mono">OPENAI_API_KEY</span>). Saving one here overrides it.
              </div>
            )}

            {state && !state.can_store_keys && (
              <div className="ai-error" style={{ marginBottom: 12 }}>
                <span className="mono">WARDN_SECRET_KEY</span> is not set, so keys cannot be
                encrypted at rest and this form is disabled. Set it and restart, or supply the
                key through the environment instead.
              </div>
            )}

            <form className="ai-form" onSubmit={save}>
              <label>
                Provider
                <select
                  className="pill"
                  value={kind}
                  onChange={(e) => {
                    setKind(e.target.value)
                    setModel('')
                    setCustomMode(false)
                  }}
                >
                  {(state?.kinds ?? ['anthropic', 'openai']).map((k) => (
                    <option key={k} value={k}>
                      {k}
                    </option>
                  ))}
                </select>
              </label>

              <label>
                Model
                <select
                  className="pill"
                  value={customMode ? '__custom__' : model}
                  onChange={(e) => {
                    if (e.target.value === '__custom__') {
                      setCustomMode(true)
                      setModel('')
                    } else {
                      setCustomMode(false)
                      setModel(e.target.value)
                    }
                  }}
                >
                  <option value="">Default · {defaultModel}</option>
                  {modelOptions.map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                  <option value="__custom__">Custom…</option>
                </select>
              </label>

              {customMode && (
                <label>
                  Custom model id
                  <input
                    className="text-input"
                    value={model}
                    placeholder={defaultModel}
                    onChange={(e) => setModel(e.target.value)}
                  />
                </label>
              )}

              <label>
                API key
                <input
                  className="text-input"
                  type="password"
                  autoComplete="off"
                  value={apiKey}
                  placeholder={
                    state?.provider?.key_last4 && state.source === 'database'
                      ? `stored - ends in ${state.provider.key_last4}`
                      : 'sk-…'
                  }
                  onChange={(e) => setApiKey(e.target.value)}
                />
              </label>

              <label>
                Base URL <span className="muted">(optional proxy or gateway)</span>
                <input
                  className="text-input"
                  value={baseURL}
                  placeholder="https://…"
                  onChange={(e) => setBaseURL(e.target.value)}
                />
              </label>

              <div className="row-actions">
                <button className="login-btn" type="submit" disabled={!state?.can_store_keys || busy === 'save'}>
                  {busy === 'save' ? 'Saving…' : 'Save'}
                </button>
                <button
                  className="ghost-btn"
                  type="button"
                  onClick={test}
                  disabled={!state?.configured || busy === 'test'}
                >
                  {busy === 'test' ? 'Testing…' : 'Test'}
                </button>
                {state?.source === 'database' && (
                  <button className="ghost-btn danger" type="button" onClick={remove} disabled={busy === 'delete'}>
                    Remove
                  </button>
                )}
              </div>
            </form>

            {error && <div className="ai-error" style={{ marginTop: 12 }}>{error}</div>}
            {notice && <div className="ai-notice" style={{ marginTop: 12 }}>{notice}</div>}
          </div>
        </div>

        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">Automatic analysis</span>
          </div>
          <div className="panel-body" style={{ padding: 0 }}>
            <div className="muted" style={{ fontSize: 13, padding: '14px 16px 4px' }}>
              When enabled, a regression on these services triggers root-cause analysis
              automatically. Ask AI stays available on every deploy either way.
              {!state?.configured && (
                <span style={{ color: 'var(--text-faint)' }}>
                  {' '}Configure a provider above for auto-analysis to actually run.
                </span>
              )}
            </div>
            {apps.length === 0 ? (
              <div className="empty" style={{ height: 120 }}>
                No services registered
              </div>
            ) : (
              <table className="data-table">
                <thead>
                  <tr>
                    <th>Service</th>
                    <th>Auto-analyze on regression</th>
                  </tr>
                </thead>
                <tbody>
                  {apps.map((a) => (
                    <tr key={a.id}>
                      <td className="mono">{a.name}</td>
                      <td>
                        <label className="ai-toggle">
                          <input
                            type="checkbox"
                            checked={!!a.ai_enabled}
                            onChange={(e) => toggleApp(a.id, e.target.checked)}
                          />
                          <span>{a.ai_enabled ? 'on' : 'off'}</span>
                        </label>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
