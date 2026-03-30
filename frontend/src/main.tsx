import './index.css'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import ErrorBoundary from './components/ErrorBoundary'
import { PrivacyProvider } from './utils/PrivacyContext'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <PrivacyProvider>
          <App />
        </PrivacyProvider>
      </BrowserRouter>
    </ErrorBoundary>
  </StrictMode>,
)
