/* eslint-disable react-refresh/only-export-components */
import { createContext, useContext, type ReactNode } from 'react'
import { usePersistentState } from './usePersistentState'

interface PrivacyContextValue {
  privacy: boolean
  togglePrivacy: () => void
}

const PrivacyContext = createContext<PrivacyContextValue>({
  privacy: false,
  togglePrivacy: () => {},
})

export function PrivacyProvider({ children }: { children: ReactNode }) {
  const [privacy, setPrivacy] = usePersistentState('privacy_mode', false)
  const togglePrivacy = () => setPrivacy((p: boolean) => !p)
  return (
    <PrivacyContext.Provider value={{ privacy, togglePrivacy }}>
      {children}
    </PrivacyContext.Provider>
  )
}

export function usePrivacy() {
  return useContext(PrivacyContext)
}
