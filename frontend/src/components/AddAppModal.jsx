import { useEffect, useState } from 'react'

// Two-phase dialog: (1) enter a name → create the app, then (2) reveal the
// generated API key exactly once. The key is what the app must send as a Bearer
// token on every deploy marker, so we make copying it deliberate and warn that
// it won't be shown again.
export default function AddAppModal({ onClose, onCreate, onCreated }) {
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [result, setResult] = useState(null) // { app, api_key }
  const [copied, setCopied] = useState(false)

  // Esc closes the dialog (only meaningful before a key is shown; after that the
  // user should copy it, but we still let them close).
  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  async function submit(e) {
    e.preventDefault()
    setErr('')
    setBusy(true)
    try {
      const data = await onCreate(name.trim())
      setResult(data)
      onCreated?.(data.app?.name)
    } catch (e2) {
      setErr(String(e2.message || e2))
    } finally {
      setBusy(false)
    }
  }

  async function copyKey() {
    try {
      await navigator.clipboard.writeText(result.api_key)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="modal-overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal" role="dialog" aria-modal="true">
        {!result ? (
          <>
            <div className="modal-head">
              <div className="modal-title">Add app / service</div>
              <div className="modal-sub">
                Registers a service and generates an API key. The app must send that key to
                report deploy markers.
              </div>
            </div>
            <form className="modal-body" onSubmit={submit}>
              <input
                className="text-input"
                autoFocus
                placeholder="e.g. test-app"
                value={name}
                onChange={(e) => setName(e.target.value)}
                spellCheck={false}
              />
              <div className="modal-sub" style={{ marginTop: -4 }}>
                3–64 chars · lowercase letters, digits and hyphens
              </div>
              {err && <div className="modal-err">{err}</div>}
              <div className="modal-actions">
                <button type="button" className="ghost-btn" onClick={onClose} disabled={busy}>
                  Cancel
                </button>
                <button
                  type="submit"
                  className="login-btn"
                  style={{ width: 'auto', padding: '10px 18px' }}
                  disabled={busy || !name.trim()}
                >
                  {busy ? 'Creating…' : 'Create'}
                </button>
              </div>
            </form>
          </>
        ) : (
          <>
            <div className="modal-head">
              <div className="modal-title">“{result.app.name}” created</div>
              <div className="modal-sub">Copy the API key now — it won’t be shown again.</div>
            </div>
            <div className="modal-body">
              <div className="key-reveal">
                <code>{result.api_key}</code>
                <button type="button" className="ghost-btn copy-btn" onClick={copyKey}>
                  {copied ? 'Copied' : 'Copy'}
                </button>
              </div>
              <div className="key-warn">
                <b>Store it securely.</b> wardn only keeps a hash. Send it as{' '}
                <code>Authorization: Bearer &lt;key&gt;</code> on every{' '}
                <code>POST /api/v1/deployments</code> for this app.
              </div>
              <div className="modal-actions">
                <button
                  type="button"
                  className="login-btn"
                  style={{ width: 'auto', padding: '10px 18px' }}
                  onClick={onClose}
                >
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
