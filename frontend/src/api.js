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

// Create a new app/service. Returns { app, api_key } — api_key is shown once.
export async function createApp(name) {
  const res = await fetch('/api/v1/apps', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ name }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `create app: ${res.status}`)
  return data
}

export async function fetchVersions(app, range = '1d', metric = 'latency_ms') {
  const params = new URLSearchParams({ app, metric, range })
  const res = await fetch(`/api/v1/versions?${params}`, opts)
  if (!res.ok) throw new Error(`versions: ${res.status}`)
  const data = await res.json()
  return data.versions ?? []
}

export async function fetchVersionSeries(app, version, range = '1d', metric = 'latency_ms') {
  const params = new URLSearchParams({ app, version, metric, range })
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

export async function fetchMetricDefinitions() {
  const res = await fetch('/api/v1/metric-definitions', opts)
  if (!res.ok) throw new Error(`metric-definitions: ${res.status}`)
  const data = await res.json()
  return data.metrics ?? []
}

// ---- AI reasoning ----

// Queue an analysis. 202 either way: if one is already running the backend
// returns it rather than starting a second (billable) call.
export async function requestAnalysis(deployId) {
  const res = await fetch(`/api/v1/deploys/${deployId}/analyze`, {
    method: 'POST',
    credentials: 'include',
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `analyze: ${res.status}`)
  return data.analysis
}

export async function fetchAnalysis(id) {
  const res = await fetch(`/api/v1/analyses/${id}`, opts)
  if (!res.ok) throw new Error(`analysis: ${res.status}`)
  const data = await res.json()
  return data.analysis
}

export async function fetchAnalyses(deployId) {
  const res = await fetch(`/api/v1/deploys/${deployId}/analyses`, opts)
  if (!res.ok) throw new Error(`analyses: ${res.status}`)
  const data = await res.json()
  return data.analyses ?? []
}

export async function fetchAIProvider() {
  const res = await fetch('/api/v1/ai/provider', opts)
  if (!res.ok) throw new Error(`ai provider: ${res.status}`)
  return res.json()
}

export async function saveAIProvider(body) {
  const res = await fetch('/api/v1/ai/provider', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `save provider: ${res.status}`)
  return data.provider
}

export async function deleteAIProvider() {
  const res = await fetch('/api/v1/ai/provider', { method: 'DELETE', credentials: 'include' })
  if (!res.ok) throw new Error(`delete provider: ${res.status}`)
}

export async function testAIProvider() {
  const res = await fetch('/api/v1/ai/provider/test', { method: 'POST', credentials: 'include' })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `test provider: ${res.status}`)
  return data
}

export async function setAppAIEnabled(appId, enabled) {
  const res = await fetch(`/api/v1/apps/${appId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ ai_enabled: enabled }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `update app: ${res.status}`)
  return data.app
}
