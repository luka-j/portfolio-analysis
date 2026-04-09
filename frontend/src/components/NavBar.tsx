import { useState, useEffect } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import { clearToken, getLLMAvailable } from '../api'
import { usePrivacy } from '../utils/PrivacyContext'
import HoverTooltip from './HoverTooltip'

export default function NavBar() {
  const navigate = useNavigate()
  const { privacy, togglePrivacy } = usePrivacy()
  const [llmAvailable, setLlmAvailable] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)

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

  const closeMobile = () => setMobileOpen(false)

  return (
    <div className="w-full pt-4 pb-1 mb-4 flex justify-center relative">
      <div className="w-full max-w-7xl px-4 md:px-8 flex items-center justify-between">

        {/* Left: Logo */}
        <div className="flex-1 flex items-center justify-start">
          <div className="flex items-center gap-3">
            <img src="/favicon.svg" alt="Portfolio Analysis" className="w-8 h-8 rounded-xl" />
            <span className="text-base font-bold text-white select-none">
              Portfolio Analysis
            </span>
          </div>
        </div>

        {/* Center: Navigation Links (desktop only) */}
        <nav className="hidden md:flex flex-1 justify-center items-center gap-10">
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
          {privacy ? (
            <div className="relative group flex items-center">
              <span className="text-slate-600 cursor-not-allowed text-sm font-medium">Tax</span>
              <HoverTooltip direction="down" className="w-42">
                Not available in private mode.
              </HoverTooltip>
            </div>
          ) : (
            <NavLink to="/tax" className={linkClass}>
              Tax
            </NavLink>
          )}
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
              <HoverTooltip direction="down" className="w-60">
                LLM features are currently unavailable. Please configure GEMINI_API_KEY.
              </HoverTooltip>
            </div>
          )}
        </nav>

        {/* Right: Privacy indicator + Logout + (mobile) hamburger */}
        <div className="flex-1 flex justify-end items-center gap-1">
          {privacy && (
            <div className="relative group">
              <button
                onClick={togglePrivacy}
                className="p-2.5 rounded-xl text-red-400/70 hover:text-red-400 hover:bg-red-500/10 transition-all duration-200"
              >
                <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/>
                  <line x1="1" y1="1" x2="23" y2="23"/>
                </svg>
              </button>
              <HoverTooltip direction="down" align="right" className="w-36 text-center">
                Disable private mode
              </HoverTooltip>
            </div>
          )}
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
          <button
            onClick={() => setMobileOpen(o => !o)}
            className="md:hidden p-2.5 rounded-xl text-slate-400 hover:text-white hover:bg-white/5 transition-all duration-200"
            aria-label="Toggle menu"
            aria-expanded={mobileOpen}
          >
            {mobileOpen ? (
              <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                <line x1="18" y1="6" x2="6" y2="18"/>
                <line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            ) : (
              <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                <line x1="3" y1="6" x2="21" y2="6"/>
                <line x1="3" y1="12" x2="21" y2="12"/>
                <line x1="3" y1="18" x2="21" y2="18"/>
              </svg>
            )}
          </button>
        </div>

      </div>

      {/* Mobile drawer panel */}
      {mobileOpen && (
        <div className="md:hidden absolute top-full left-0 right-0 z-40 mt-1 mx-4 rounded-2xl bg-[#13151f] border border-white/8 shadow-2xl backdrop-blur-2xl overflow-hidden">
          <nav className="flex flex-col divide-y divide-white/5">
            <NavLink to="/" end onClick={closeMobile} className={({ isActive }) => `px-5 py-3.5 text-sm font-medium ${isActive ? 'text-white bg-indigo-500/10' : 'text-slate-300 hover:bg-white/5'}`}>
              Dashboard
            </NavLink>
            <NavLink to="/portfolio" onClick={closeMobile} className={({ isActive }) => `px-5 py-3.5 text-sm font-medium ${isActive ? 'text-white bg-indigo-500/10' : 'text-slate-300 hover:bg-white/5'}`}>
              Portfolio
            </NavLink>
            <NavLink to="/analysis" onClick={closeMobile} className={({ isActive }) => `px-5 py-3.5 text-sm font-medium ${isActive ? 'text-white bg-indigo-500/10' : 'text-slate-300 hover:bg-white/5'}`}>
              Analysis
            </NavLink>
            <NavLink to="/breakdown" onClick={closeMobile} className={({ isActive }) => `px-5 py-3.5 text-sm font-medium ${isActive ? 'text-white bg-indigo-500/10' : 'text-slate-300 hover:bg-white/5'}`}>
              Breakdown
            </NavLink>
            {privacy ? (
              <span className="px-5 py-3.5 text-sm font-medium text-slate-600 cursor-not-allowed">
                Tax <span className="text-[10px] font-normal">(unavailable in private mode)</span>
              </span>
            ) : (
              <NavLink to="/tax" onClick={closeMobile} className={({ isActive }) => `px-5 py-3.5 text-sm font-medium ${isActive ? 'text-white bg-indigo-500/10' : 'text-slate-300 hover:bg-white/5'}`}>
                Tax
              </NavLink>
            )}
            {llmAvailable ? (
              <NavLink to="/llm" onClick={closeMobile} className={({ isActive }) => `px-5 py-3.5 text-sm font-medium ${isActive ? 'text-white bg-indigo-500/10' : 'text-slate-300 hover:bg-white/5'}`}>
                LLM
              </NavLink>
            ) : (
              <span className="px-5 py-3.5 text-sm font-medium text-slate-600 cursor-not-allowed">
                LLM <span className="text-[10px] font-normal">(configure GEMINI_API_KEY)</span>
              </span>
            )}
          </nav>
        </div>
      )}
    </div>
  )
}
