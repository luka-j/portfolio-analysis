import { useState, useRef, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useScenario } from '../context/ScenarioContext'
import { deleteScenario, updateScenario, getScenario, createScenario } from '../api'
import ConfirmDialog from './ConfirmDialog'

export default function ScenarioPill() {
  const { active, scenarios, setActive, setCompare, refresh } = useScenario()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [pendingDeleteId, setPendingDeleteId] = useState<number | null>(null)
  const [deleting, setDeleting] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  const activeScenario = scenarios.find(s => s.id === active) ?? null
  const label = active === null ? 'Real' : (activeScenario?.name || `Scenario ${active}`)
  const isNonReal = active !== null

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  function openDelete(id: number, e: React.MouseEvent) {
    e.stopPropagation()
    setPendingDeleteId(id)
  }

  async function handleDelete() {
    if (pendingDeleteId === null) return
    setDeleting(true)
    try {
      await deleteScenario(pendingDeleteId)
      setActive(null)
      setCompare(null)
      setPendingDeleteId(null)
      await refresh()
    } finally {
      setDeleting(false)
    }
  }

  async function handleTogglePin(id: number, pinned: boolean, e: React.MouseEvent) {
    e.stopPropagation()
    await updateScenario(id, { pinned: !pinned })
    await refresh()
  }

  function handleEdit(id: number, e: React.MouseEvent) {
    e.stopPropagation()
    setOpen(false)
    navigate(`/scenario/edit?id=${id}`)
  }

  async function handleExport(id: number, e: React.MouseEvent) {
    e.stopPropagation()
    try {
      const detail = await getScenario(id)
      const payload = JSON.stringify({ name: detail.name, pinned: detail.pinned, spec: detail.spec }, null, 2)
      const blob = new Blob([payload], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      const safe = (detail.name || `scenario-${id}`).replace(/[^\w.-]+/g, '_')
      a.href = url
      a.download = `${safe}.scenario.json`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    } catch { /* swallow — user can retry */ }
  }

  const fileInputRef = useRef<HTMLInputElement>(null)
  async function handleImportClick() {
    fileInputRef.current?.click()
  }

  async function handleImportFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    try {
      const text = await file.text()
      const parsed = JSON.parse(text)
      if (!parsed?.spec) throw new Error('Missing "spec" field')
      const saved = await createScenario(parsed.spec, parsed.name ?? file.name.replace(/\.scenario\.json$/i, ''), !!parsed.pinned)
      await refresh()
      setActive(saved.id)
      setOpen(false)
    } catch (err) {
      alert('Import failed: ' + ((err as Error).message || 'invalid file'))
    } finally {
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen(o => !o)}
        className={`flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-medium border transition-all duration-200 ${
          isNonReal
            ? 'border-amber-400/40 text-amber-300 bg-amber-500/10 hover:bg-amber-500/20'
            : 'border-white/8 text-slate-400 hover:text-slate-200 hover:border-white/20'
        }`}
      >
        <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z"/>
          <circle cx="12" cy="12" r="3"/>
        </svg>
        {label}
        <svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="6 9 12 15 18 9"/>
        </svg>
      </button>

      {open && (
        <div className="absolute top-full mt-2 left-0 z-50 w-64 rounded-2xl bg-panel border border-white/8 shadow-2xl backdrop-blur-2xl overflow-hidden">
          {/* Real option */}
          <button
            onClick={() => { setActive(null); setOpen(false) }}
            className={`w-full flex items-center gap-2 px-4 py-3 text-sm text-left transition-colors ${
              active === null ? 'text-white bg-white/5' : 'text-slate-300 hover:bg-white/5'
            }`}
          >
            <span className="flex-1 font-medium">Real portfolio</span>
            {active === null && (
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <polyline points="20 6 9 17 4 12"/>
              </svg>
            )}
          </button>

          {scenarios.length > 0 && <div className="border-t border-white/5" />}

          {/* Scenario rows */}
          {scenarios.map(s => (
            <div
              key={s.id}
              onClick={() => { setActive(s.id); setOpen(false) }}
              className={`flex items-center gap-2 px-4 py-2.5 text-sm cursor-pointer transition-colors group ${
                active === s.id ? 'text-amber-300 bg-amber-500/10' : 'text-slate-300 hover:bg-white/5'
              }`}
            >
              <span className="flex-1 truncate">{s.name || `Scenario ${s.id}`}</span>
              <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                {/* Pin toggle */}
                <button
                  onClick={(e) => handleTogglePin(s.id, s.pinned, e)}
                  title={s.pinned ? 'Unpin' : 'Pin'}
                  className="p-1 rounded hover:bg-white/10 transition-colors"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill={s.pinned ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <line x1="12" y1="17" x2="12" y2="22"/>
                    <path d="M5 17h14v-1.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1v4.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24Z"/>
                  </svg>
                </button>
                {/* Edit */}
                <button
                  onClick={(e) => handleEdit(s.id, e)}
                  title="Edit"
                  className="p-1 rounded hover:bg-white/10 transition-colors"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
                    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
                  </svg>
                </button>
                {/* Export */}
                <button
                  onClick={(e) => handleExport(s.id, e)}
                  title="Export JSON"
                  className="p-1 rounded hover:bg-white/10 transition-colors"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
                    <polyline points="7 10 12 15 17 10"/>
                    <line x1="12" y1="15" x2="12" y2="3"/>
                  </svg>
                </button>
                {/* Delete */}
                <button
                  onClick={(e) => openDelete(s.id, e)}
                  title="Delete"
                  className="p-1 rounded hover:bg-red-500/20 hover:text-red-400 transition-colors"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <line x1="18" y1="6" x2="6" y2="18"/>
                    <line x1="6" y1="6" x2="18" y2="18"/>
                  </svg>
                </button>
              </div>
              {active === s.id && (
                <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" className="text-amber-300 shrink-0">
                  <polyline points="20 6 9 17 4 12"/>
                </svg>
              )}
            </div>
          ))}

          <div className="border-t border-white/5" />

          {/* New scenario */}
          <button
            onClick={() => { setOpen(false); navigate('/scenario/edit') }}
            className="w-full flex items-center gap-2 px-4 py-3 text-sm text-slate-400 hover:text-white hover:bg-white/5 transition-colors"
          >
            <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <line x1="12" y1="5" x2="12" y2="19"/>
              <line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
            New scenario
          </button>

          {/* Import scenario */}
          <button
            onClick={handleImportClick}
            className="w-full flex items-center gap-2 px-4 py-3 text-sm text-slate-400 hover:text-white hover:bg-white/5 transition-colors border-t border-white/5"
          >
            <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
              <polyline points="17 8 12 3 7 8"/>
              <line x1="12" y1="3" x2="12" y2="15"/>
            </svg>
            Import JSON…
          </button>
          <input ref={fileInputRef} type="file" accept="application/json,.json" className="hidden" onChange={handleImportFile} />
        </div>
      )}
      {pendingDeleteId !== null && (
        <ConfirmDialog
          title="Delete scenario?"
          message="This cannot be undone."
          confirmLabel="Delete"
          busy={deleting}
          onConfirm={handleDelete}
          onCancel={() => setPendingDeleteId(null)}
        />
      )}
    </div>
  )
}
