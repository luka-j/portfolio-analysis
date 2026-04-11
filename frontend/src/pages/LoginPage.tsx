import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { setToken, verifyToken } from '../api'

export default function LoginPage() {
  const [token, setTokenValue] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!token.trim()) {
      setError('Please enter your token')
      return
    }
    setLoading(true)
    setError('')
    setToken(token.trim())

    try {
      const valid = await verifyToken()
      if (valid) {
        navigate('/')
      } else {
        setError('Invalid token. Please try again.')
        setToken('')
      }
    } catch {
      setError('Invalid token. Please try again.')
      setToken('')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="w-full max-w-md">
        {/* Logo */}
        <div className="flex flex-col items-center mb-8">
          <img src="/favicon.svg" alt="Portfolio Analysis" className="w-16 h-16 mb-4 rounded-2xl" />
          <h1 className="text-3xl font-bold text-indigo-400">
            Portfolio Analysis
          </h1>
          <p className="text-slate-400 mt-2 text-sm">Portfolio Analysis Dashboard</p>
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit} className="bg-[#1a1d2e] rounded-2xl p-8 border border-[#2a2e42] shadow-xl">
          <label htmlFor="token" className="block text-sm font-medium text-slate-300 mb-2">
            Access Token
          </label>
          <input
            id="token"
            type="password"
            value={token}
            onChange={(e) => setTokenValue(e.target.value)}
            placeholder="Enter your user token"
            className="w-full px-4 py-3 bg-[#0f1117] border border-[#2a2e42] rounded-xl text-slate-200 placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/50 focus:border-indigo-500 transition-all"
            autoFocus
          />

          {error && (
            <p className="mt-3 text-sm text-red-400 bg-red-500/10 px-3 py-2 rounded-lg">
              {error}
            </p>
          )}

          <button
            type="submit"
            disabled={loading}
            className="w-full mt-6 px-4 py-3.5 bg-indigo-600 text-white font-semibold rounded-xl hover:bg-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/50 transition-all duration-200 disabled:opacity-50 disabled:cursor-not-allowed shadow-lg shadow-indigo-500/20"
          >
            {loading ? (
              <span className="flex items-center justify-center gap-2">
                <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" fill="none" />
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                </svg>
                Verifying...
              </span>
            ) : 'Sign In'}
          </button>
        </form>

        <p className="text-center text-slate-500 text-xs mt-6">
          Token is used to identify your portfolio data on the server.
        </p>
      </div>
    </div>
  )
}
