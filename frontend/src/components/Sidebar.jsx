// The left rail. Dashboards / Deploys / Alerting are wired; Home/Explore/Admin stay decorative.

const Icon = {
  home: (
    <svg width="16" height="16" viewBox="0 0 20 20" fill="none">
      <path d="M3 9L10 3L17 9" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M5 8V16H15V8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  ),
  grid: (
    <svg width="16" height="16" viewBox="0 0 20 20" fill="none">
      <rect x="3" y="3" width="6" height="6" rx="1.2" stroke="currentColor" strokeWidth="1.6" />
      <rect x="11" y="3" width="6" height="6" rx="1.2" stroke="currentColor" strokeWidth="1.6" />
      <rect x="3" y="11" width="6" height="6" rx="1.2" stroke="currentColor" strokeWidth="1.6" />
      <rect x="11" y="11" width="6" height="6" rx="1.2" stroke="currentColor" strokeWidth="1.6" />
    </svg>
  ),
  deploys: (
    <svg width="16" height="16" viewBox="0 0 20 20" fill="none">
      <path d="M4 14L9 8L12 11L16 5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="16" cy="5" r="1.6" fill="currentColor" />
    </svg>
  ),
  alert: (
    <svg width="16" height="16" viewBox="0 0 20 20" fill="none">
      <path d="M10 3L18 16H2L10 3Z" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
      <path d="M10 8V11" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      <circle cx="10" cy="13.4" r="0.9" fill="currentColor" />
    </svg>
  ),
  explore: (
    <svg width="16" height="16" viewBox="0 0 20 20" fill="none">
      <circle cx="9" cy="9" r="5.5" stroke="currentColor" strokeWidth="1.6" />
      <path d="M13 13L17 17" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  ),
  admin: (
    <svg width="16" height="16" viewBox="0 0 20 20" fill="none">
      <circle cx="10" cy="10" r="6" stroke="currentColor" strokeWidth="1.6" />
      <circle cx="10" cy="10" r="1.8" fill="currentColor" />
    </svg>
  ),
}

const ITEMS = [
  { icon: 'home', label: 'Home', page: null },
  { icon: 'grid', label: 'Dashboards', page: 'dashboards' },
  { icon: 'deploys', label: 'Deploys', page: 'deploys' },
  { icon: 'alert', label: 'Alerting', page: 'alerting' },
  { icon: 'explore', label: 'Explore', page: null },
]

export default function Sidebar({ user, page, onNavigate, onLogout }) {
  const name = user?.username ?? '—'
  const role = user?.role ?? ''
  const initials = name.slice(0, 2).toUpperCase()
  return (
    <aside className="sidebar">
      <div className="brand">
        <svg width="26" height="18" viewBox="0 0 76 60" fill="none">
          <polyline points="10,12 24,46 38,26 52,46 66,12" stroke="#5BC98A" strokeWidth="6" strokeLinecap="round" strokeLinejoin="round" />
          <circle cx="66" cy="12" r="6" fill="#5BC98A" />
        </svg>
        <span className="wordmark">wardn</span>
      </div>

      {ITEMS.map((item) => {
        const on = item.page && item.page === page
        return (
          <button
            key={item.label}
            className={`nav${on ? ' on' : ''}`}
            type="button"
            onClick={() => item.page && onNavigate?.(item.page)}
          >
            {Icon[item.icon]}
            {item.label}
          </button>
        )
      })}

      <div className="spacer" />

      <button className="nav" type="button">
        {Icon.admin}
        Admin
      </button>
      <div className="user">
        <div className="avatar">{initials}</div>
        <div className="who">
          <span className="name">{name}</span>
          <span className="role">{role}</span>
        </div>
        <button className="signout" type="button" onClick={onLogout} title="Sign out" aria-label="Sign out">
          <svg width="16" height="16" viewBox="0 0 20 20" fill="none">
            <path d="M8 4H5a1 1 0 00-1 1v10a1 1 0 001 1h3" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M12 14l3-4-3-4M15 10H8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
      </div>
    </aside>
  )
}
