import { useState } from 'react'
import { login } from '../api.js'

export default function Login({ onSuccess }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const user = await login(username, password)
      onSuccess(user)
    } catch {
      setError('Invalid username or password')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <form className="login-card fade-in" onSubmit={submit}>
        <div className="login-brand">
          <svg width="30" height="21" viewBox="0 0 76 60" fill="none">
            <polyline points="10,12 24,46 38,26 52,46 66,12" stroke="#5BC98A" strokeWidth="6" strokeLinecap="round" strokeLinejoin="round" />
            <circle cx="66" cy="12" r="6" fill="#5BC98A" />
          </svg>
          <span className="wordmark">wardn</span>
        </div>

        <div className="login-title">Sign in</div>
        <div className="login-sub">Access the deploy dashboard</div>

        <label className="login-field">
          <span>Username</span>
          <input
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            autoFocus
          />
        </label>

        <label className="login-field">
          <span>Password</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </label>

        {error && <div className="login-error">{error}</div>}

        <button className="login-btn" type="submit" disabled={busy || !username || !password}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>

        <div className="login-hint">Dev login — admin / admin@12345</div>
      </form>
    </div>
  )
}
