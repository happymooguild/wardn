import { useEffect, useMemo, useState } from 'react'

// Two-phase dialog: (1) enter a name → create the app, then (2) reveal the
// generated API key exactly once + show ready-to-paste snippets for wiring the
// deploy marker into CI/GitOps. The key is what the app must send as a Bearer
// token on every marker, so we make copying it deliberate and warn once.
export default function AddAppModal({ onClose, onCreate, onCreated }) {
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [result, setResult] = useState(null) // { app, api_key }
  const [copied, setCopied] = useState(false)
  const [tab, setTab] = useState('curl')
  const [snipCopied, setSnipCopied] = useState(false)

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

  async function copyText(text, setFlag) {
    try {
      await navigator.clipboard.writeText(text)
      setFlag(true)
      setTimeout(() => setFlag(false), 1500)
    } catch {
      setFlag(false)
    }
  }

  // Pre-filled snippets for the created app. Marker endpoint is same-origin as
  // the dashboard (nginx proxies /api to the backend); swap for your wardn URL
  // if they differ. `${...}` shell/CI tokens are intentionally literal.
  const snippets = useMemo(() => {
    if (!result) return {}
    const app = result.app.name
    const key = result.api_key
    const base = window.location.origin
    return {
      curl: [
        'curl -fsS -X POST ' + base + '/api/v1/deployments \\',
        '  -H "Authorization: Bearer ' + key + '" \\',
        "  -H 'Content-Type: application/json' \\",
        '  -d \'{"app":"' + app + '","version":"v1.0.0","environment":"production","source":"ci"}\'',
      ].join('\n'),
      github: [
        '# .github/workflows/deploy.yml — a step AFTER your rollout succeeds.',
        '# Store the key as a repo secret named WARDN_API_KEY.',
        '- name: Notify wardn of deploy',
        '  env:',
        '    WARDN_API_KEY: ${{ secrets.WARDN_API_KEY }}',
        '  run: |',
        '    curl -fsS -X POST ' + base + '/api/v1/deployments \\',
        '      -H "Authorization: Bearer $WARDN_API_KEY" \\',
        "      -H 'Content-Type: application/json' \\",
        '      -d "{\\"app\\":\\"' + app + '\\",\\"version\\":\\"${GITHUB_REF_NAME}\\",\\"source\\":\\"ci\\"}"',
      ].join('\n'),
      argocd: [
        '# 1) Key as a Secret in the argocd namespace',
        'apiVersion: v1',
        'kind: Secret',
        'metadata: { name: wardn-key, namespace: argocd }',
        'stringData: { wardn-api-key: ' + key + ' }',
        '---',
        '# 2) Add to the argocd-notifications-cm ConfigMap',
        'service.webhook.wardn: |',
        '  url: ' + base + '/api/v1/deployments',
        '  headers:',
        '    - name: Authorization',
        '      value: Bearer $wardn-api-key',
        'template.app-deployed-wardn: |',
        '  webhook:',
        '    wardn:',
        '      method: POST',
        '      body: |',
        '        {"app":"' + app + '","version":"{{.app.status.sync.revision}}","source":"argocd"}',
        "trigger.on-deployed: |",
        "  - when: app.status.operationState.phase in ['Succeeded'] and app.status.health.status == 'Healthy'",
        '    oncePer: app.status.sync.revision',
        '    send: [app-deployed-wardn]',
        '# 3) Annotate the Application:',
        '#   notifications.argoproj.io/subscribe.on-deployed.wardn: ""',
      ].join('\n'),
    }
  }, [result])

  const TABS = [
    ['curl', 'curl'],
    ['github', 'GitHub Actions'],
    ['argocd', 'ArgoCD'],
  ]

  return (
    <div className="modal-overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={`modal${result ? ' modal-wide' : ''}`} role="dialog" aria-modal="true">
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
                <button type="button" className="ghost-btn copy-btn" onClick={() => copyText(result.api_key, setCopied)}>
                  {copied ? 'Copied' : 'Copy'}
                </button>
              </div>
              <div className="key-warn">
                <b>Store it securely.</b> wardn only keeps a hash — send it as{' '}
                <code>Authorization: Bearer &lt;key&gt;</code> on every deploy marker.
              </div>

              <div className="section-label" style={{ marginTop: 4 }}>SEND YOUR FIRST DEPLOY MARKER</div>
              <div className="modal-sub" style={{ marginTop: -6 }}>
                Fire this from your pipeline <b>after a successful rollout</b>. Set{' '}
                <code>version</code> to your release (git tag, semver, build no.).
              </div>
              <div className="snippet-tabs">
                {TABS.map(([id, label]) => (
                  <button
                    key={id}
                    type="button"
                    className={`snippet-tab${tab === id ? ' on' : ''}`}
                    onClick={() => setTab(id)}
                  >
                    {label}
                  </button>
                ))}
                <button
                  type="button"
                  className="ghost-btn snippet-copy"
                  onClick={() => copyText(snippets[tab], setSnipCopied)}
                >
                  {snipCopied ? 'Copied' : 'Copy'}
                </button>
              </div>
              <pre className="snippet"><code>{snippets[tab]}</code></pre>

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
