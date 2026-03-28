import { NavLink, useNavigate } from 'react-router-dom'
import { clearToken } from '../api'

export default function NavBar() {
  const navigate = useNavigate()

  const handleLogout = () => {
    clearToken()
    navigate('/login')
  }

  const linkClass = ({ isActive }: { isActive: boolean }) =>
    `nav-link text-sm font-medium transition-colors duration-200 ${
      isActive ? 'text-white active' : 'text-slate-500 hover:text-slate-200'
    }`

  return (
    <div className="w-full pt-4 pb-1 mb-4 flex justify-center">
      <div className="w-full max-w-7xl px-8 flex items-center justify-between">

        {/* Left: Logo */}
        <div className="flex-1 flex items-center justify-start">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-xl bg-linear-to-br from-indigo-500 to-purple-600 flex items-center justify-center text-white font-bold text-xs shadow-lg shadow-indigo-500/20">
              G
            </div>
            <span className="text-base font-bold text-white select-none">
              Gofolio
            </span>
          </div>
        </div>

        {/* Center: Navigation Links */}
        <nav className="flex-1 flex justify-center items-center gap-10">
          <NavLink to="/" className={linkClass} end>
            Dashboard
          </NavLink>
          <NavLink to="/portfolio" className={linkClass}>
            Portfolio
          </NavLink>
          <NavLink to="/analysis" className={linkClass}>
            Analysis
          </NavLink>
          <NavLink to="/breakdown" className={linkClass}>
            Breakdown
          </NavLink>
          <NavLink to="/tax" className={linkClass}>
            Tax
          </NavLink>
        </nav>

        {/* Right: Logout */}
        <div className="flex-1 flex justify-end">
          <button
            onClick={handleLogout}
            className="p-2.5 rounded-xl text-slate-500 hover:text-red-400 hover:bg-red-500/10 transition-all duration-200"
            title="Logout"
          >
            <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
              <polyline points="16 17 21 12 16 7" />
              <line x1="21" y1="12" x2="9" y2="12" />
            </svg>
          </button>
        </div>

      </div>
    </div>
  )
}
