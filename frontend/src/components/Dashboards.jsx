import { useEffect, useState } from 'react'
import DashboardView from './DashboardView.jsx'
import CreateDashboardModal from './CreateDashboardModal.jsx'
import { fetchDashboards, createDashboard, deleteDashboard } from '../api.js'

// Landing view: a plain list of dashboard names (built-in + custom). Clicking one
// opens it. The button up top creates a custom dashboard from any SigNoz metric.
export default function Dashboards({ apps, onAuthError }) {
  const [dashboards, setDashboards] = useState([])
  const [openId, setOpenId] = useState(null)
  const [showCreate, setShowCreate] = useState(false)
  const [err, setErr] = useState('')

  const load = () =>
    fetchDashboards()
      .then(setDashboards)
      .catch((e) => onAuthError?.(e))

  useEffect(() => {
    load()
  }, [])

  const open = dashboards.find((d) => d.id === openId)
  if (open) {
    return <DashboardView cfg={open} apps={apps} onBack={() => setOpenId(null)} onAuthError={onAuthError} />
  }

  async function onDelete(d) {
    setErr('')
    try {
      await deleteDashboard(d.id)
      load()
    } catch (e) {
      setErr(String(e.message || e))
    }
  }

  return (
    <div className="content-inner fade-in">
      <div className="section-label" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <span>DASHBOARDS · {dashboards.length}</span>
        <button type="button" className="add-btn" onClick={() => setShowCreate(true)}>
          <span className="plus">+</span> Create custom dashboard
        </button>
      </div>

      {showCreate && (
        <CreateDashboardModal
          existing={dashboards}
          onClose={() => setShowCreate(false)}
          onCreate={createDashboard}
          onCreated={() => load()}
        />
      )}

      {err && <div className="modal-err" style={{ marginBottom: 10 }}>{err}</div>}

      <div className="dash-list">
        {dashboards.map((d) => (
          <div key={d.id} className="dash-list-item" role="button" tabIndex={0} onClick={() => setOpenId(d.id)}>
            <span className="dash-list-name">{d.name}</span>
            <span className="dash-list-right">
              {!d.builtin && <span className="dash-list-tag">custom</span>}
              {!d.builtin && (
                <button
                  type="button"
                  className="dash-del"
                  title="Delete dashboard"
                  onClick={(e) => {
                    e.stopPropagation()
                    onDelete(d)
                  }}
                >
                  ×
                </button>
              )}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}
