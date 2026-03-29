import { useState, useEffect } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import { clearToken, getLLMAvailable } from '../api'

export default function NavBar() {
  const navigate = useNavigate()
  const [llmAvailable, setLlmAvailable] = useState(false)

  useEffect(() => {
    getLLMAvailable()
      .then(res => setLlmAvailable(res.available))
      .catch(() => setLlmAvailable(false))
  }, [])

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
          {llmAvailable ? (
            <NavLink to="/llm" className={linkClass}>
              LLM
            </NavLink>
          ) : (
            <div className="relative group flex items-center">
              <span 
                className="text-slate-600 cursor-not-allowed text-sm font-medium transition-colors duration-200"
              >
                LLM
              </span>
              <div className="absolute top-full mt-2.5 w-60 px-3 py-2.5 bg-[#12151f] border border-[#2a2e42]/80 rounded-xl text-[10px] text-slate-400 leading-relaxed pointer-events-none opacity-0 group-hover:opacity-100 transition-opacity z-50 shadow-2xl left-1/2 -translate-x-1/2">
                LLM features are currently unavailable. Please configure GEMINI_API_KEY.
              </div>
            </div>
          )}
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
