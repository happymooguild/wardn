// Thin API client. Paths are relative (/api/...) so the same build works behind
// the Vite dev proxy and behind nginx in the pod. credentials:'include' ensures
// the session cookie rides along.

const opts = { credentials: 'include' }

// ---- auth ----

export async function login(username, password) {
  const res = await fetch('/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ username, password }),
  })
  if (!res.ok) throw new Error('login failed')
  const data = await res.json()
  return data.user
}

export async function logout() {
  await fetch('/api/v1/auth/logout', { method: 'POST', credentials: 'include' })
}

export async function me() {
  const res = await fetch('/api/v1/auth/me', opts)
  if (res.status === 401) return null
  if (!res.ok) throw new Error(`me: ${res.status}`)
  const data = await res.json()
  return data.user
}

// ---- data ----

export async function fetchApps() {
  const res = await fetch('/api/v1/apps', opts)
  if (!res.ok) throw new Error(`apps: ${res.status}`)
  const data = await res.json()
  return data.apps ?? []
}

export async function fetchVersions(app, metric = 'latency_ms') {
  const params = new URLSearchParams({ app, metric })
  const res = await fetch(`/api/v1/versions?${params}`, opts)
  if (!res.ok) throw new Error(`versions: ${res.status}`)
  const data = await res.json()
  return data.versions ?? []
}

export async function fetchVersionSeries(app, version, metric = 'latency_ms') {
  const params = new URLSearchParams({ app, version, metric })
  const res = await fetch(`/api/v1/metrics?${params}`, opts)
  if (!res.ok) throw new Error(`series: ${res.status}`)
  const data = await res.json()
  return data.points ?? []
}

export async function fetchDeploys(app) {
  const params = new URLSearchParams({ app })
  const res = await fetch(`/api/v1/deploys?${params}`, opts)
  if (!res.ok) throw new Error(`deploys: ${res.status}`)
  const data = await res.json()
  return data.deploys ?? []
}

export async function fetchDeploy(id) {
  const res = await fetch(`/api/v1/deploys/${id}`, opts)
  if (!res.ok) throw new Error(`deploy: ${res.status}`)
  return res.json()
}

export async function fetchAlerts(appId) {
  const res = await fetch(`/api/v1/apps/${appId}/alerts`, opts)
  if (!res.ok) throw new Error(`alerts: ${res.status}`)
  const data = await res.json()
  return data.alerts ?? []
}

export async function createAlert(appId, body) {
  const res = await fetch(`/api/v1/apps/${appId}/alerts`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || `create alert: ${res.status}`)
  }
  const data = await res.json()
  return data.alert
}

export async function deleteAlert(id) {
  const res = await fetch(`/api/v1/alerts/${id}`, { method: 'DELETE', credentials: 'include' })
  if (!res.ok) throw new Error(`delete alert: ${res.status}`)
}

export async function testAlert(id) {
  const res = await fetch(`/api/v1/alerts/${id}/test`, { method: 'POST', credentials: 'include' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || `test alert: ${res.status}`)
  }
}

export async function fetchDeliveries(appId) {
  const res = await fetch(`/api/v1/apps/${appId}/alert-deliveries`, opts)
  if (!res.ok) throw new Error(`deliveries: ${res.status}`)
  const data = await res.json()
  return data.deliveries ?? []
}
