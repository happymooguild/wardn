import { useEffect, useState } from 'react'
import Sidebar from './components/Sidebar.jsx'
import Login from './components/Login.jsx'
import Deploys from './components/Deploys.jsx'
import Dashboards from './components/Dashboards.jsx'
import Alerting from './components/Alerting.jsx'
import AISettings from './components/AISettings.jsx'
import Home from './components/Home.jsx'
import Explore from './components/Explore.jsx'
import { fetchApps, me, logout } from './api.js'

const PAGE_META = {
  home: { title: 'Home', sub: 'what wardn can do, and how your apps are doing right now' },
  dashboards: { title: 'Dashboards', sub: 'per-version metrics from SigNoz, snapshotted around each deploy' },
  deploys: { title: 'Deploys', sub: 'deploy markers and before/after analysis' },
  alerting: { title: 'Alerting', sub: 'regression alerts and delivery channels' },
  ai: { title: 'AI Settings', sub: 'provider credentials and automatic root-cause analysis' },
  explore: { title: 'Explore', sub: 'everything wardn can do, in one place' },
}

export default function App() {
  const [user, setUser] = useState(null)
  const [checking, setChecking] = useState(true)
  const [page, setPage] = useState('home')

  const [apps, setApps] = useState([])
  const [app, setApp] = useState('')
  const [error, setError] = useState('')

  // On mount, ask the backend who we are. 401 -> show the login screen.
  useEffect(() => {
    me()
      .then((u) => setUser(u))
      .catch(() => setUser(null))
      .finally(() => setChecking(false))
  }, [])

  // A 401 from any data call means the session lapsed — bounce to login.
  const onAuthError = (e) => {
    if (String(e).includes('401')) setUser(null)
    else setError(String(e))
  }

  // Load apps once signed in. Exposed so AI Settings can refresh the list
  // after toggling a per-app setting.
  const loadApps = () =>
    fetchApps()
      .then((list) => {
        setApps(list)
        setApp((cur) => (cur || (list.length ? list[0].name : '')))
      })
      .catch(onAuthError)

  useEffect(() => {
    if (!user) return
    loadApps()
  }, [user])

  async function handleLogout() {
    await logout()
    setUser(null)
    setApps([])
    setApp('')
    setError('')
  }

  // --- gate ---
  if (checking) return <div className="login-wrap" />
  if (!user) return <Login onSuccess={setUser} />

  const meta = PAGE_META[page] || PAGE_META.dashboards
  const sub = meta.sub

  return (
    <div className="app">
      <Sidebar user={user} page={page} onNavigate={setPage} onLogout={handleLogout} />

      <div className="main">
        <header className="header">
          <div className="crumbs">
            <span>Home</span>
            {page !== 'home' && (
              <>
                <span className="sep">/</span>
                <span className="here">{meta.title}</span>
              </>
            )}
          </div>
          <div className="header-main">
            <div className="header-title">
              <h1>{meta.title}</h1>
              <span className="header-sub">{sub}</span>
            </div>
            <div className="header-controls" />
          </div>
        </header>

        <div className="content">
          {page === 'home' && <Home apps={apps} onNavigate={setPage} onAuthError={onAuthError} />}
          {page === 'deploys' && (
            <Deploys apps={apps} onAuthError={onAuthError} onAppCreated={() => loadApps()} />
          )}
          {page === 'alerting' && <Alerting apps={apps} onAuthError={onAuthError} />}
          {page === 'ai' && <AISettings apps={apps} onAppsChanged={loadApps} onAuthError={onAuthError} />}
          {page === 'explore' && <Explore onNavigate={setPage} />}
          {page === 'dashboards' && <Dashboards apps={apps} onAuthError={onAuthError} />}
        </div>
      </div>
    </div>
  )
}
