import { Routes, Route, Navigate } from 'react-router-dom'
import { hasToken } from './api'
import LoginPage from './pages/LoginPage'
import LandingPage from './pages/LandingPage'
import PortfolioPage from './pages/PortfolioPage'
import AnalysisPage from './pages/AnalysisPage'
import BreakdownPage from './pages/BreakdownPage'
import TaxPage from './pages/TaxPage'

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  if (!hasToken()) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/" element={<ProtectedRoute><LandingPage /></ProtectedRoute>} />
      <Route path="/portfolio" element={<ProtectedRoute><PortfolioPage /></ProtectedRoute>} />
      <Route path="/analysis" element={<ProtectedRoute><AnalysisPage /></ProtectedRoute>} />
      <Route path="/breakdown" element={<ProtectedRoute><BreakdownPage /></ProtectedRoute>} />
      <Route path="/tax" element={<ProtectedRoute><TaxPage /></ProtectedRoute>} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
