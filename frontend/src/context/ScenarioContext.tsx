import { createContext, useContext, useEffect, useState, useCallback, useRef } from 'react'
import { listScenarios, type ScenarioSummary } from '../api'
import { usePersistentState } from '../utils/usePersistentState'

interface ScenarioContextValue {
  active: number | null         // null = Real portfolio
  compare: number | null        // null = compare off
  scenarios: ScenarioSummary[]
  setActive: (id: number | null) => void
  setCompare: (id: number | null) => void
  refresh: () => Promise<void>
}

const ScenarioContext = createContext<ScenarioContextValue>({
  active: null,
  compare: null,
  scenarios: [],
  setActive: () => {},
  setCompare: () => {},
  refresh: async () => {},
})

export function ScenarioProvider({ children }: { children: React.ReactNode }) {
  const [active, setActive] = usePersistentState<number | null>('scenario_active', null)
  const [compare, setCompare] = usePersistentState<number | null>('scenario_compare', null)
  const [scenarios, setScenarios] = useState<ScenarioSummary[]>([])
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  const refresh = useCallback(async () => {
    try {
      const list = await listScenarios()
      if (mountedRef.current) setScenarios(list ?? [])
    } catch {
      // swallow — user may not be logged in yet
    }
  }, [])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void refresh()
  }, [refresh])

  // If a persisted active/compare scenario no longer exists after refresh, reset it.
  useEffect(() => {
    const ids = new Set(scenarios.map(s => s.id))
    if (active !== null && !ids.has(active)) setActive(null)
    if (compare !== null && !ids.has(compare)) setCompare(null)
  }, [scenarios, active, compare, setActive, setCompare])

  return (
    <ScenarioContext.Provider value={{ active, compare, scenarios, setActive, setCompare, refresh }}>
      {children}
    </ScenarioContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export function useScenario() {
  return useContext(ScenarioContext)
}
