import { useEffect } from 'react'
import { Routes, Route, Navigate, useNavigate } from 'react-router-dom'
import { hasToken } from './api'
import ErrorBoundary from './components/ErrorBoundary'
import LoginPage from './pages/LoginPage'
import LandingPage from './pages/LandingPage'
import PortfolioPage from './pages/PortfolioPage'
import AnalysisPage from './pages/AnalysisPage'
import BreakdownPage from './pages/BreakdownPage'
import TaxPage from './pages/TaxPage'
import LLMPage from './pages/LLMPage'

/** Listens for 401 events dispatched by the api layer and redirects to /login. */
function UnauthorizedListener() {
  const navigate = useNavigate()
  useEffect(() => {
    const handle = () => navigate('/login', { replace: true })
    window.addEventListener('portfolio:unauthorized', handle)
    return () => window.removeEventListener('portfolio:unauthorized', handle)
  }, [navigate])
  return null
}

/** Wraps a route in auth guard + per-page ErrorBoundary. */
function ProtectedRoute({ children }: { children: React.ReactNode }) {
  if (!hasToken()) {
    return <Navigate to="/login" replace />
  }
  return <ErrorBoundary>{children}</ErrorBoundary>
}

export default function App() {
  return (
    <>
      <UnauthorizedListener />
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/" element={<ProtectedRoute><LandingPage /></ProtectedRoute>} />
        <Route path="/portfolio" element={<ProtectedRoute><PortfolioPage /></ProtectedRoute>} />
        <Route path="/analysis" element={<ProtectedRoute><AnalysisPage /></ProtectedRoute>} />
        <Route path="/breakdown" element={<ProtectedRoute><BreakdownPage /></ProtectedRoute>} />
        <Route path="/tax" element={<ProtectedRoute><TaxPage /></ProtectedRoute>} />
        <Route path="/llm" element={<ProtectedRoute><LLMPage /></ProtectedRoute>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </>
  )
}
