import { NavLink, useNavigate } from 'react-router-dom'
import { clearToken } from '../api'

export default function NavBar() {
  const navigate = useNavigate()

  const handleLogout = () => {
    clearToken()
    navigate('/login')
  }

  const linkClass = ({ isActive }: { isActive: boolean }) =>
    `px-6 py-3.5 rounded-xl text-sm font-semibold transition-all duration-200 ${
      isActive
        ? 'bg-indigo-500/20 text-indigo-400'
        : 'text-slate-400 hover:text-slate-200 hover:bg-white/5'
    }`

  return (
    <nav className="flex items-center justify-between px-6 py-3 border-b border-[#2a2e42] bg-[#13152180] backdrop-blur-xl sticky top-0 z-50">
      <div className="flex items-center gap-2">
        <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-indigo-500 to-purple-600 flex items-center justify-center text-white font-bold text-sm">
          G
        </div>
        <span className="text-lg font-semibold bg-gradient-to-r from-indigo-400 to-purple-400 bg-clip-text text-transparent">
          Gofolio
        </span>
      </div>

      <div className="flex items-center gap-1">
        <NavLink to="/" className={linkClass} end>Dashboard</NavLink>
        <NavLink to="/portfolio" className={linkClass}>Portfolio</NavLink>
        <NavLink to="/analysis" className={linkClass}>Analysis</NavLink>
        <NavLink to="/breakdown" className={linkClass}>Breakdown</NavLink>
        <NavLink to="/tax" className={linkClass}>Tax</NavLink>
      </div>

      <button
        onClick={handleLogout}
        className="px-6 py-3 text-sm font-semibold text-slate-400 hover:text-red-400 hover:bg-red-500/10 rounded-xl transition-all duration-200"
      >
        Logout
      </button>
    </nav>
  )
}
