// Thin API client. Paths are relative (/api/...) so the same build works behind
// the Vite dev proxy and behind nginx in the pod.

export async function fetchApps() {
  const res = await fetch('/api/v1/apps')
  if (!res.ok) throw new Error(`apps: ${res.status}`)
  const data = await res.json()
  return data.apps ?? []
}

// Per-version percentile profiles — the version comparison chart.
export async function fetchVersions(app, metric = 'latency_ms') {
  const params = new URLSearchParams({ app, metric })
  const res = await fetch(`/api/v1/versions?${params}`)
  if (!res.ok) throw new Error(`versions: ${res.status}`)
  const data = await res.json()
  return data.versions ?? []
}

// Raw samples for one version — the drill-down time-series.
export async function fetchVersionSeries(app, version, metric = 'latency_ms') {
  const params = new URLSearchParams({ app, version, metric })
  const res = await fetch(`/api/v1/metrics?${params}`)
  if (!res.ok) throw new Error(`series: ${res.status}`)
  const data = await res.json()
  return data.points ?? []
}
