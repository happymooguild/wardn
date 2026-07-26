import { useEffect, useState } from 'react'
import { fetchDeploys, createApp } from '../api.js'
import AddAppModal from './AddAppModal.jsx'
import AppDeploys from './AppDeploys.jsx'

const POLL_MS = 5000

// Deploys landing page: a list of every registered service (not a single-app
// view like the dashboard). Each row shows the app's latest deploy at a glance;
// clicking one drills into its full deploy history + analysis. New services are
// added right here.
export default function Deploys({ apps, onAuthError, onAppCreated }) {
  const [opened, setOpened] = useState(null) // app name being drilled into
  const [showAdd, setShowAdd] = useState(false)
  const [latest, setLatest] = useState({}) // app name -> latest deploy | null

  // Pull each app's most recent deploy so the list can show status at a glance.
  useEffect(() => {
    if (opened || apps.length === 0) return
    let alive = true
    const load = async () => {
      try {
        const entries = await Promise.all(
          apps.map(async (a) => {
            const list = await fetchDeploys(a.name)
            return [a.name, list[0] || null]
          })
        )
        if (alive) setLatest(Object.fromEntries(entries))
      } catch (e) {
        if (alive) onAuthError?.(e)
      }
    }
    load()
    const id = setInterval(load, POLL_MS)
    return () => {
      alive = false
      clearInterval(id)
    }
  }, [apps, opened])

  if (opened) {
    return <AppDeploys appName={opened} onBack={() => setOpened(null)} onAuthError={onAuthError} />
  }

  return (
    <div className="content-inner fade-in">
      <div className="section-label" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <span>SERVICES · {apps.length}</span>
        <button type="button" className="add-btn" onClick={() => setShowAdd(true)}>
          <span className="plus">+</span> Add app / service
        </button>
      </div>

      {showAdd && (
        <AddAppModal
          onClose={() => setShowAdd(false)}
          onCreate={createApp}
          onCreated={(name) => onAppCreated?.(name)}
        />
      )}

      <div className="panel">
        <div className="panel-body" style={{ padding: 0 }}>
          {apps.length === 0 ? (
            <div className="empty" style={{ height: 160 }}>
              No services yet - click “Add app / service” to register one.
            </div>
          ) : (
            <table className="data-table apps-table">
              <thead>
                <tr>
                  <th>Service</th>
                  <th>Environment</th>
                  <th>Latest deploy</th>
                  <th>Status</th>
                  <th>When</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {apps.map((a) => {
                  const d = latest[a.name]
                  return (
                    <tr key={a.id} onClick={() => setOpened(a.name)}>
                      <td>
                        <span className="app-name">{a.name}</span>
                      </td>
                      <td className="muted">{a.environment}</td>
                      <td className="mono">{d ? d.version : '-'}</td>
                      <td>
                        {d ? (
                          <span className={`badge status-${d.status}`}>{d.status}</span>
                        ) : (
                          <span className="muted">no deploys</span>
                        )}
                      </td>
                      <td className="muted">{d ? formatTime(d.deployed_at) : '-'}</td>
                      <td className="muted" style={{ textAlign: 'right' }}>
                        View →
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  )
}

function formatTime(iso) {
  if (!iso) return '-'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}
