import { useEffect, Suspense, lazy } from 'react'
import { Routes, Route, Navigate, useNavigate } from 'react-router-dom'
import { hasToken } from './api'
import ErrorBoundary from './components/ErrorBoundary'
import { ScenarioProvider } from './context/ScenarioContext'

const LoginPage = lazy(() => import('./pages/LoginPage'))
const LandingPage = lazy(() => import('./pages/LandingPage'))
const PortfolioPage = lazy(() => import('./pages/PortfolioPage'))
const AnalysisPage = lazy(() => import('./pages/AnalysisPage'))
const BreakdownPage = lazy(() => import('./pages/BreakdownPage'))
const TaxPage = lazy(() => import('./pages/TaxPage'))
const LLMPage = lazy(() => import('./pages/LLMPage'))
const ScenarioEditPage = lazy(() => import('./pages/ScenarioEditPage'))
const TransactionsPage = lazy(() => import('./pages/TransactionsPage'))

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
      <ScenarioProvider>
        <Suspense fallback={
          <div className="flex h-screen items-center justify-center bg-bg text-slate-400">
            <svg className="animate-spin h-8 w-8 mr-3 opacity-50" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" fill="none" />
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
            </svg>
            Loading...
          </div>
        }>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<ProtectedRoute><LandingPage /></ProtectedRoute>} />
            <Route path="/portfolio" element={<ProtectedRoute><PortfolioPage /></ProtectedRoute>} />
            <Route path="/transactions" element={<ProtectedRoute><TransactionsPage /></ProtectedRoute>} />
            <Route path="/analysis" element={<ProtectedRoute><AnalysisPage /></ProtectedRoute>} />
            <Route path="/breakdown" element={<ProtectedRoute><BreakdownPage /></ProtectedRoute>} />
            <Route path="/tax" element={<ProtectedRoute><TaxPage /></ProtectedRoute>} />
            <Route path="/llm" element={<ProtectedRoute><LLMPage /></ProtectedRoute>} />
            <Route path="/scenario/edit" element={<ProtectedRoute><ScenarioEditPage /></ProtectedRoute>} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Suspense>
      </ScenarioProvider>
    </>
  )
}
