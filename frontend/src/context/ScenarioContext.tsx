import { createContext, useContext, useEffect, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
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
  const queryClient = useQueryClient()

  const { data: scenarios = [] } = useQuery({
    queryKey: ['scenarios'],
    queryFn: async () => {
      try {
        const list = await listScenarios()
        return list ?? []
      } catch {
        return []
      }
    },
  })

  const refresh = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: ['scenarios'] })
  }, [queryClient])

  // If a persisted active/compare scenario no longer exists after refresh, reset it.
  useEffect(() => {
    const ids = new Set(scenarios.map(s => s.id))
    if (active !== null && !ids.has(active)) setActive(null)
    if (compare !== null && compare !== 0 && !ids.has(compare)) setCompare(null)
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
