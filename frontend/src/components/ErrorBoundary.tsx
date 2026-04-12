import { Component, type ReactNode } from 'react'

interface Props {
  children: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
}

export default class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="min-h-screen bg-bg flex items-center justify-center p-8">
          <div className="max-w-md w-full bg-surface rounded-2xl border border-red-500/30 p-8 text-center">
            <h2 className="text-xl font-bold text-red-400 mb-3">Something went wrong</h2>
            <p className="text-slate-400 text-sm mb-6">
              {this.state.error?.message || 'An unexpected error occurred.'}
            </p>
            <button
              onClick={() => {
                this.setState({ hasError: false, error: null })
                window.location.href = '/'
              }}
              className="px-6 py-2.5 bg-indigo-500 text-white text-sm font-semibold rounded-xl hover:bg-indigo-600 transition-all"
            >
              Return to Dashboard
            </button>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}
