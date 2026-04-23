import { useNavigate } from 'react-router-dom'
import { useScenario } from '../context/ScenarioContext'

export default function ScenarioBanner() {
  const { active, scenarios, setActive, setCompare } = useScenario()
  const navigate = useNavigate()

  if (active === null) return null

  const scenario = scenarios.find(s => s.id === active)
  const name = scenario?.name || `Scenario ${active}`

  return (
    <div className="w-full flex items-center justify-center gap-3 px-4 py-2 bg-amber-500/10 border-b border-amber-400/20 text-amber-300 text-xs">
      <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z"/>
        <circle cx="12" cy="12" r="3"/>
      </svg>
      <span>Viewing scenario: <strong className="font-semibold">{name}</strong></span>
      <span className="text-amber-400/50">·</span>
      <button
        onClick={() => navigate(`/scenario/edit?id=${active}`)}
        className="underline underline-offset-2 hover:text-amber-200 transition-colors"
      >
        Edit
      </button>
      <span className="text-amber-400/50">·</span>
      <button
        onClick={() => { setActive(null); setCompare(null) }}
        className="underline underline-offset-2 hover:text-amber-200 transition-colors"
      >
        Back to Real
      </button>
    </div>
  )
}
