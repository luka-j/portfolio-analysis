import { useState, useEffect, useCallback } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import PageLayout from '../components/PageLayout'
import SegmentedControl from '../components/SegmentedControl'
import DatePicker from '../components/DatePicker'
import Spinner from '../components/Spinner'
import ErrorAlert from '../components/ErrorAlert'
import {
  getPortfolioValue,
  getScenario,
  createScenario,
  updateScenario,
  deleteScenario,
  type PositionValue,
  type ScenarioSpec,
  type Adjustment,
  type Basket,
  type BasketItem,
  type BacktestConfig,
  type BaseMode,
  type BasketMode,
  type RebalanceMode,
  type ContributionCadence,
} from '../api'
import { useScenario } from '../context/ScenarioContext'
import { usePersistentState } from '../utils/usePersistentState'
import { formatDate, CURRENCIES } from '../utils/format'
import SelectInput from '../components/SelectInput'
import NumberInput from '../components/NumberInput'
import ConfirmDialog from '../components/ConfirmDialog'
import HoverTooltip from '../components/HoverTooltip'

type EditorMode = 'modify' | 'basket' | 'backtest' | 'redirect'

const MODE_OPTIONS = [
  { label: 'Adjust Trades', value: 'modify' as EditorMode },
  { label: 'Custom Basket', value: 'basket' as EditorMode },
  { label: 'Target Allocation', value: 'redirect' as EditorMode },
  { label: 'Historical Backtest', value: 'backtest' as EditorMode },
]

// Per-row adjustment kind for the Adjust Trades editor. Maps to AdjustmentAction.
type RowAction = 'none' | 'sell_pct' | 'sell_qty' | 'sell_all' | 'buy_amount'
const ROW_ACTION_LABEL: Record<RowAction, string> = {
  none: 'No change',
  sell_pct: 'Sell %',
  sell_qty: 'Sell qty',
  sell_all: 'Sell all',
  buy_amount: 'Buy $',
}
const ROW_ACTION_OPTIONS: RowAction[] = ['none', 'sell_pct', 'sell_qty', 'sell_all', 'buy_amount']

interface RowAdjustment {
  action: RowAction
  value: number
  date: string       // YYYY-MM-DD; blank = today
}

interface CustomTrade {
  symbol: string
  exchange: string
  action: 'buy_amount' | 'sell_qty'
  value: number
  currency: string
  date: string
}

const CONTRIBUTION_OPTIONS = [
  { label: 'None', value: 'none' as ContributionCadence },
  { label: 'Monthly', value: 'monthly' as ContributionCadence },
  { label: 'Quarterly', value: 'quarterly' as ContributionCadence },
  { label: 'Annually', value: 'annually' as ContributionCadence },
]

const REBALANCE_OPTIONS = [
  { label: 'None', value: 'none' as RebalanceMode },
  { label: 'Monthly', value: 'monthly' as RebalanceMode },
  { label: 'Quarterly', value: 'quarterly' as RebalanceMode },
  { label: 'Annually', value: 'annually' as RebalanceMode },
  { label: 'Threshold', value: 'threshold' as RebalanceMode },
]

const BASKET_MODE_OPTIONS = [
  { label: 'By Weight', value: 'weight' as BasketMode },
  { label: 'By Quantity', value: 'quantity' as BasketMode },
]


function today(): string {
  return formatDate(new Date())
}

function fiveYearsAgo(): string {
  const d = new Date()
  d.setFullYear(d.getFullYear() - 5)
  return formatDate(d)
}

// ---- Basket editor (shared between Custom Basket and Backtest modes) ----

function BasketEditor({
  items, mode, notional, notionalCurrency, acquiredAt,
  onItemsChange, onModeChange, onNotionalChange, onNotionalCurrencyChange, onAcquiredAtChange,
  hideNotional, hideAcquiredAt
}: {
  items: BasketItem[]
  mode: BasketMode
  notional: number
  notionalCurrency: string
  acquiredAt: string
  onItemsChange: (items: BasketItem[]) => void
  onModeChange: (m: BasketMode) => void
  onNotionalChange: (v: number) => void
  onNotionalCurrencyChange: (c: string) => void
  onAcquiredAtChange: (d: string) => void
  hideNotional?: boolean
  hideAcquiredAt?: boolean
}) {
  function addRow() {
    onItemsChange([...items, { symbol: '', currency: 'USD', weight: 0, quantity: 0 }])
  }

  function removeRow(i: number) {
    onItemsChange(items.filter((_, idx) => idx !== i))
  }

  function updateRow(i: number, patch: Partial<BasketItem>) {
    onItemsChange(items.map((item, idx) => idx === i ? { ...item, ...patch } : item))
  }

  const weightSum = mode === 'weight'
    ? items.reduce((s, item) => s + (item.weight ?? 0), 0)
    : null
  const weightOk = weightSum === null || Math.abs(weightSum - 1) < 0.001

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-4 flex-wrap">
        <SegmentedControl label="Mode" options={BASKET_MODE_OPTIONS} value={mode} onChange={onModeChange} />
        {mode === 'weight' && (
          <>
            {!hideNotional && (
              <div className="flex flex-col gap-1">
                <label className="text-[9px] font-black text-slate-500 uppercase tracking-[0.2em]">Notional Value</label>
                <div className="w-32">
                  <NumberInput
                    value={notional ? notional.toString() : ''}
                    onChange={v => onNotionalChange(parseFloat(v) || 0)}
                    placeholder="10000"
                    min={0}
                  />
                </div>
              </div>
            )}
            <div className="flex flex-col gap-1">
              <label className="text-[9px] font-black text-slate-500 uppercase tracking-[0.2em]">Currency</label>
              <SelectInput
                options={CURRENCIES}
                value={notionalCurrency}
                onChange={onNotionalCurrencyChange}
              />
            </div>
          </>
        )}
        {!hideAcquiredAt && (
          <div className="flex flex-col gap-1">
            <label className="text-[9px] font-black text-slate-500 uppercase tracking-[0.2em]">Acquired At</label>
            <DatePicker value={acquiredAt} onChange={onAcquiredAtChange} />
          </div>
        )}
      </div>

      <div className="rounded-2xl border border-border-dim overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-white/5 text-[10px] font-black uppercase tracking-widest text-slate-500">
              <th className="text-left px-4 py-2.5">Symbol</th>
              <th className="text-right px-4 py-2.5">{mode === 'weight' ? 'Weight (0–1)' : 'Quantity'}</th>
              <th className="text-right px-4 py-2.5">Currency</th>
              <th className="text-right px-4 py-2.5">Cost Basis</th>
              <th className="px-3 py-2.5"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {items.map((item, i) => (
              <tr key={i} className="bg-surface/30">
                <td className="px-4 py-2">
                  <input
                    type="text"
                    value={item.symbol}
                    onChange={e => updateRow(i, { symbol: e.target.value.toUpperCase() })}
                    placeholder="Yahoo ticker"
                    title="Yahoo Finance ticker (e.g. AAPL, VUAA.L, CSPX.L)"
                    className="w-28 bg-bg border border-border-dim rounded-lg px-2 py-1.5 text-slate-100 font-mono text-sm focus:outline-none focus:border-indigo-500/50"
                  />
                </td>
                <td className="px-4 py-2 text-right">
                  <div className="w-28">
                    <NumberInput
                      value={(mode === 'weight' ? (item.weight ?? 0) : (item.quantity ?? 0)).toString()}
                      onChange={v => {
                        const n = parseFloat(v) || 0
                        updateRow(i, mode === 'weight' ? { weight: n } : { quantity: n })
                      }}
                      min={0}
                      step={mode === 'weight' ? 0.01 : 1}
                    />
                  </div>
                </td>
                <td className="px-4 py-2 text-right">
                  <SelectInput
                    options={CURRENCIES}
                    value={item.currency}
                    onChange={v => updateRow(i, { currency: v })}
                  />
                </td>
                <td className="px-4 py-2 text-right">
                  <div className="w-24">
                    <NumberInput
                      value={item.cost_basis != null ? item.cost_basis.toString() : ''}
                      onChange={v => updateRow(i, { cost_basis: v !== '' ? parseFloat(v) || undefined : undefined })}
                      placeholder="auto"
                      min={0}
                      step={0.01}
                    />
                  </div>
                </td>
                <td className="px-3 py-2 text-right">
                  <button
                    onClick={() => removeRow(i)}
                    className="p-1.5 rounded-lg text-slate-500 hover:text-red-400 hover:bg-red-500/10 transition-colors"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                    </svg>
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <button
          onClick={addRow}
          className="w-full px-4 py-2.5 text-sm text-slate-400 hover:text-white hover:bg-white/5 transition-colors flex items-center gap-2 border-t border-white/5"
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
          </svg>
          Add position
        </button>
      </div>

      {mode === 'weight' && weightSum !== null && !weightOk && (
        <p className="text-amber-400 text-xs">
          Weights sum to {(weightSum * 100).toFixed(1)}% — must equal 100%.
        </p>
      )}
    </div>
  )
}

// ---- Main page ----

export default function ScenarioEditPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { setActive, refresh } = useScenario()

  const editId = params.get('id') ? parseInt(params.get('id')!) : null

  const [loading, setLoading] = useState(editId !== null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = useState(false)

  // Form state
  const [name, setName] = useState('')
  const [pinned, setPinned] = usePersistentState('scenario_edit_pinned', true)
  const [mode, setMode] = useState<EditorMode>('modify')

  // Modify-real state
  const [holdings, setHoldings] = useState<PositionValue[]>([])
  const [holdingsLoading, setHoldingsLoading] = useState(false)
  const [rowAdjustments, setRowAdjustments] = useState<Record<string, RowAdjustment>>({})
  const [customTrades, setCustomTrades] = useState<CustomTrade[]>([])
  const [modifyBase, setModifyBase] = useState<BaseMode>('real')

  // Basket state
  const [basketMode, setBasketMode] = useState<BasketMode>('weight')
  const [basketItems, setBasketItems] = useState<BasketItem[]>([{ symbol: '', currency: 'USD', weight: 0 }])
  const [notional, setNotional] = useState(10000)
  const [notionalCurrency, setNotionalCurrency] = useState('USD')
  const [acquiredAt, setAcquiredAt] = useState(today())

  // Redirect state
  const [redirectBasketItems, setRedirectBasketItems] = useState<BasketItem[]>([{ symbol: '', currency: 'USD', weight: 0 }])
  const [redirectNotionalCurrency, setRedirectNotionalCurrency] = useState('USD')

  // Backtest state
  const [btStartDate, setBtStartDate] = useState(fiveYearsAgo())
  const [btInitial, setBtInitial] = useState(10000)
  const [btCurrency, setBtCurrency] = useState('USD')
  const [btContribution, setBtContribution] = useState<ContributionCadence>('none')
  const [btContributionAmt, setBtContributionAmt] = useState(500)
  const [btRebalance, setBtRebalance] = useState<RebalanceMode>('none')
  const [btThreshold, setBtThreshold] = useState(5)
  const [btBasketMode, setBtBasketMode] = useState<BasketMode>('weight')
  const [btBasketItems, setBtBasketItems] = useState<BasketItem[]>([{ symbol: '', currency: 'USD', weight: 0 }])
  const [btNotional, setBtNotional] = useState(10000)
  const [btNotionalCurrency, setBtNotionalCurrency] = useState('USD')

  // Load existing scenario for edit
  useEffect(() => {
    if (editId === null) return
    let cancelled = false
    setLoading(true)
    getScenario(editId)
      .then(s => {
        if (cancelled) return
        setName(s.name ?? '')
        setPinned(s.pinned ?? false)
        const spec = s.spec
        if (spec.backtest) {
          setMode('backtest')
          setBtStartDate(spec.backtest.start_date)
          setBtInitial(spec.backtest.initial_amount)
          setBtCurrency(spec.backtest.currency)
          setBtContribution(spec.backtest.contribution)
          setBtContributionAmt(spec.backtest.contribution_amount)
          setBtRebalance(spec.backtest.rebalance)
          setBtThreshold(spec.backtest.rebalance_threshold * 100)
          if (spec.basket) {
            setBtBasketMode(spec.basket.mode)
            setBtBasketItems(spec.basket.items)
            setBtNotional(spec.basket.notional_value ?? 10000)
            setBtNotionalCurrency(spec.basket.notional_currency ?? 'USD')
          }
        } else if (spec.basket && spec.base === 'empty') {
          setMode('basket')
          setBasketMode(spec.basket.mode)
          setBasketItems(spec.basket.items)
          setNotional(spec.basket.notional_value ?? 10000)
          setNotionalCurrency(spec.basket.notional_currency ?? 'USD')
          setAcquiredAt(spec.basket.acquired_at ?? today())
        } else if (spec.base === 'redirect' && spec.basket) {
          setMode('redirect')
          setRedirectBasketItems(spec.basket.items)
          setRedirectNotionalCurrency(spec.basket.notional_currency ?? 'USD')
        } else {
          setMode('modify')
          setModifyBase(spec.base ?? 'real')
          if (spec.adjustments) {
            const rows: Record<string, RowAdjustment> = {}
            const custom: CustomTrade[] = []
            for (const adj of spec.adjustments) {
              // A "row adjustment" targets a real holding we'll match by symbol.
              // A "custom trade" is anything that doesn't — we surface it in the add-trade list.
              const row: RowAdjustment = {
                action: (adj.action === 'buy' ? 'buy_amount' : adj.action) as RowAction,
                value: adj.value ?? 0,
                date: adj.date ?? '',
              }
              if (adj.action === 'buy') {
                custom.push({
                  symbol: adj.symbol, exchange: adj.exchange || '', action: 'buy_amount',
                  value: adj.value, currency: adj.currency || 'USD', date: adj.date ?? '',
                })
              } else {
                const key = adj.exchange ? `${adj.symbol}@${adj.exchange}` : adj.symbol
                rows[key] = row
              }
            }
            setRowAdjustments(rows)
            setCustomTrades(custom)
          }
        }
      })
      .catch(e => { if (!cancelled) setError(e.message) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [editId, setPinned])

  // Load holdings for modify-real mode
  const loadHoldings = useCallback(async () => {
    setHoldingsLoading(true)
    try {
      const result = await getPortfolioValue('USD', 'historical', false, null)
      setHoldings(result.positions.filter(p => p.quantity > 0))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setHoldingsLoading(false)
    }
  }, [])

  useEffect(() => {
    if (mode === 'modify' && holdings.length === 0) {
      loadHoldings()
    }
  }, [mode, holdings.length, loadHoldings])

  function buildSpec(): ScenarioSpec {
    if (mode === 'modify') {
      const adjustments: Adjustment[] = []
      // Per-holding row actions.
      for (const [key, row] of Object.entries(rowAdjustments)) {
        if (row.action === 'none') continue
        const [symbol, exchange] = key.split('@')
        const adjBase = { symbol, ...(exchange ? { exchange } : {}), ...(row.date ? { date: row.date } : {}) }
        if (row.action === 'sell_all') {
          adjustments.push({ ...adjBase, action: 'sell_all', value: 0 })
        } else if (row.action === 'sell_pct') {
          if (row.value <= 0) continue
          adjustments.push({ ...adjBase, action: 'sell_pct', value: row.value })
        } else if (row.action === 'sell_qty') {
          if (row.value <= 0) continue
          adjustments.push({ ...adjBase, action: 'sell_qty', value: row.value })
        } else if (row.action === 'buy_amount') {
          if (row.value <= 0) continue
          adjustments.push({ ...adjBase, action: 'buy', value: row.value })
        }
      }
      // Arbitrary custom trades (buy $, sell qty for any symbol — not tied to a current holding).
      for (const t of customTrades) {
        if (!t.symbol.trim() || t.value <= 0) continue
        const adjBase = { symbol: t.symbol.toUpperCase(), ...(t.exchange ? { exchange: t.exchange.toUpperCase() } : {}), currency: t.currency, ...(t.date ? { date: t.date } : {}) }
        if (t.action === 'buy_amount') {
          adjustments.push({ ...adjBase, action: 'buy', value: t.value })
        } else {
          adjustments.push({ ...adjBase, action: 'sell_qty', value: t.value })
        }
      }
      return { base: modifyBase, adjustments }
    }

    if (mode === 'basket') {
      const basket: Basket = {
        mode: basketMode,
        items: basketItems.filter(i => i.symbol.trim()),
        notional_value: basketMode === 'weight' ? notional : undefined,
        notional_currency: basketMode === 'weight' ? notionalCurrency : undefined,
        acquired_at: acquiredAt,
      }
      return { base: 'empty' as BaseMode, basket }
    }

    if (mode === 'redirect') {
      const basket: Basket = {
        mode: 'weight',
        items: redirectBasketItems.filter(i => i.symbol.trim()),
        notional_currency: redirectNotionalCurrency,
      }
      return { base: 'redirect' as BaseMode, basket }
    }

    // backtest
    const basket: Basket = {
      mode: btBasketMode,
      items: btBasketItems.filter(i => i.symbol.trim()),
      notional_value: btBasketMode === 'weight' ? btNotional : undefined,
      notional_currency: btBasketMode === 'weight' ? btNotionalCurrency : undefined,
    }
    const backtest: BacktestConfig = {
      start_date: btStartDate,
      initial_amount: btInitial,
      currency: btCurrency,
      contribution: btContribution,
      contribution_amount: btContributionAmt,
      rebalance: btRebalance,
      rebalance_threshold: btRebalance === 'threshold' ? btThreshold / 100 : 0,
    }
    return { base: 'empty' as BaseMode, basket, backtest }
  }

  async function handleSave() {
    setSaving(true)
    setError(null)
    try {
      const spec = buildSpec()
      let saved
      if (editId !== null) {
        saved = await updateScenario(editId, { name, pinned, spec })
      } else {
        saved = await createScenario(spec, name, pinned)
      }
      await refresh()
      setActive(saved.id)
      navigate(-1)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  async function doDelete() {
    if (!editId) return
    setSaving(true)
    try {
      await deleteScenario(editId)
      await refresh()
      setPendingDelete(false)
      navigate(-1)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  // ---- Client-side validation ----
  // Compute a list of human-readable error strings based on the current editor state.
  // An empty array means the spec is valid and Save should be enabled.
  const validationErrors: string[] = (() => {
    const errs: string[] = []

    if (mode === 'basket') {
      const filledItems = basketItems.filter(i => i.symbol.trim())
      if (filledItems.length === 0) {
        errs.push('Add at least one position with a symbol.')
      }
      if (basketMode === 'weight') {
        const sum = basketItems.reduce((s, i) => s + (i.weight ?? 0), 0)
        if (Math.abs(sum - 1) >= 0.001) {
          errs.push(`Weights sum to ${(sum * 100).toFixed(1)}% — must equal 100%.`)
        }
        if (!notional || notional <= 0) {
          errs.push('Notional value must be greater than 0.')
        }
      }
    }

    if (mode === 'redirect') {
      const filledItems = redirectBasketItems.filter(i => i.symbol.trim())
      if (filledItems.length === 0) {
        errs.push('Add at least one target allocation with a symbol.')
      }
      const sum = redirectBasketItems.reduce((s, i) => s + (i.weight ?? 0), 0)
      if (Math.abs(sum - 1) >= 0.001) {
        errs.push(`Weights sum to ${(sum * 100).toFixed(1)}% — must equal 100%.`)
      }
    }

    if (mode === 'backtest') {
      const filledItems = btBasketItems.filter(i => i.symbol.trim())
      if (filledItems.length === 0) {
        errs.push('Add at least one position to the allocation basket.')
      }
      if (btBasketMode === 'weight') {
        const sum = btBasketItems.reduce((s, i) => s + (i.weight ?? 0), 0)
        if (Math.abs(sum - 1) >= 0.001) {
          errs.push(`Allocation weights sum to ${(sum * 100).toFixed(1)}% — must equal 100%.`)
        }
      }
      if (!btInitial || btInitial <= 0) {
        errs.push('Initial amount must be greater than 0.')
      }
      if (!btStartDate) {
        errs.push('Backtest start date is required.')
      }
    }

    return errs
  })()

  if (loading) {
    return (
      <PageLayout>
        <div className="flex items-center justify-center h-64"><Spinner /></div>
      </PageLayout>
    )
  }

  return (
    <PageLayout maxWidth="max-w-3xl">
      <div className="w-full space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <h1 className="text-xl font-bold text-white">
            {editId ? 'Edit Scenario' : 'New Scenario'}
          </h1>
          {editId && (
            <button
              onClick={() => setPendingDelete(true)}
              disabled={saving}
              className="text-xs text-red-400 hover:text-red-300 transition-colors"
            >
              Delete
            </button>
          )}
        </div>

        {/* Name + Pin */}
        <div className="flex items-center gap-4">
          <div className="flex-1">
            <label className="block text-[9px] font-black text-slate-500 uppercase tracking-[0.2em] mb-1.5">Name</label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="My scenario"
              className="w-full bg-bg border border-border-dim rounded-xl px-4 py-2.5 text-slate-100 text-sm focus:outline-none focus:border-indigo-500/50 transition-colors"
            />
          </div>
          <div className="flex flex-col gap-1.5 pt-5">
            <label className="flex items-center gap-2 cursor-pointer select-none relative group">
              <input
                type="checkbox"
                checked={pinned}
                onChange={e => setPinned(e.target.checked)}
                className="w-4 h-4 rounded accent-amber-400"
              />
              <span className="text-sm text-slate-300">Pin (never evict)</span>
              <HoverTooltip align="center" className="whitespace-nowrap">
                Prevents automatic eviction after 7 days of inactivity
              </HoverTooltip>
            </label>
          </div>
        </div>

        {/* Mode selector */}
        <div className="flex justify-center">
          <SegmentedControl label="Scenario type" options={MODE_OPTIONS} value={mode} onChange={setMode} />
        </div>

        {error && <ErrorAlert message={error} />}

        {/* ---- Adjust Trades ---- */}
        {mode === 'modify' && (
          <div className="space-y-4">
            <p className="text-sm text-slate-400">
              Adjust existing holdings and/or add custom trades. Per-row dates let you simulate a historical
              sale or purchase; leave blank for today's price.
            </p>

            <div className="flex justify-center">
              <SegmentedControl
                label="Base"
                options={[
                  { label: 'From real portfolio', value: 'real' as BaseMode },
                  { label: 'From empty', value: 'empty' as BaseMode },
                ]}
                value={modifyBase}
                onChange={setModifyBase}
              />
            </div>

            {modifyBase === 'real' && (
              <>
                {holdingsLoading ? (
                  <div className="flex items-center justify-center h-32"><Spinner /></div>
                ) : holdings.length === 0 ? (
                  <p className="text-slate-500 text-sm">No holdings found.</p>
                ) : (
                  <div className="rounded-2xl border border-border-dim overflow-hidden">
                    <table className="w-full table-fixed text-sm">
                      <thead>
                        <tr className="border-b border-white/5 text-[10px] font-black uppercase tracking-widest text-slate-500">
                          <th className="text-left px-4 py-2.5">Symbol</th>
                          <th className="text-right px-3 py-2.5 w-16">Qty</th>
                          <th className="text-right px-3 py-2.5 w-20">Value</th>
                          <th className="text-left px-3 py-2.5 w-40">Action</th>
                          <th className="text-right px-3 py-2.5 w-20">Amount</th>
                          <th className="text-left px-3 py-2.5 w-36">Date</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-white/5">
                        {holdings.map(p => {
                          const key = p.listing_exchange ? `${p.symbol}@${p.listing_exchange}` : p.symbol
                          const row = rowAdjustments[key] ?? { action: 'none' as RowAction, value: 0, date: '' }
                          const active = row.action !== 'none'
                          const needsValue = row.action === 'sell_pct' || row.action === 'sell_qty' || row.action === 'buy_amount'
                          return (
                            <tr key={key} className={active ? 'bg-amber-500/5' : 'bg-surface/30'}>
                              <td className="px-4 py-3 max-w-0">
                                <div className="relative group flex items-baseline gap-1.5 min-w-0">
                                  <span className="font-mono text-slate-100 shrink-0">
                                    {p.symbol}
                                    {p.listing_exchange && <span className="text-slate-500 text-[10px] ml-1">{p.listing_exchange}</span>}
                                  </span>
                                  {p.name && <span className="text-slate-500 text-xs truncate">{p.name}</span>}
                                  {p.name && (
                                    <HoverTooltip align="left" className="whitespace-nowrap font-sans">
                                      {p.name}
                                    </HoverTooltip>
                                  )}
                                </div>
                              </td>
                              <td className="px-3 py-3 text-right text-slate-400 tabular-nums text-xs">{p.quantity.toFixed(2)}</td>
                              <td className="px-3 py-3 text-right text-slate-300 tabular-nums text-xs">
                                {p.value.toLocaleString('en-US', { maximumFractionDigits: 0 })}
                              </td>
                              <td className="px-3 py-3">
                                <SelectInput
                                  options={ROW_ACTION_OPTIONS.map(a => a)}
                                  labels={ROW_ACTION_OPTIONS.map(a => ROW_ACTION_LABEL[a])}
                                  value={row.action}
                                  onChange={v => setRowAdjustments(prev => ({ ...prev, [key]: { ...row, action: v as RowAction } }))}
                                />
                              </td>
                              <td className="px-3 py-3 text-right">
                                {needsValue ? (
                                  <NumberInput
                                    value={row.value.toString()}
                                    onChange={v => setRowAdjustments(prev => ({ ...prev, [key]: { ...row, value: parseFloat(v) || 0 } }))}
                                    min={0}
                                    step={row.action === 'sell_pct' ? 1 : 0.01}
                                    placeholder={row.action === 'sell_pct' ? '%' : row.action === 'sell_qty' ? 'qty' : '$'}
                                  />
                                ) : (
                                  <span className="text-slate-500 text-xs">—</span>
                                )}
                              </td>
                              <td className="px-3 py-3">
                                {active ? (
                                  <DatePicker value={row.date} onChange={d => setRowAdjustments(prev => ({ ...prev, [key]: { ...row, date: d } }))} />
                                ) : (
                                  <span className="text-slate-500 text-xs">—</span>
                                )}
                              </td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
              </>
            )}

            {/* Custom trades: add buy/sell of arbitrary symbols on specific dates. */}
            <div className="rounded-2xl border border-border-dim overflow-hidden">
              <div className="px-4 py-2.5 flex items-center justify-between border-b border-white/5">
                <span className="text-[10px] font-black uppercase tracking-widest text-slate-500">Custom trades</span>
                <span className="text-[10px] text-slate-600">Use Yahoo Finance tickers for new symbols</span>
              </div>
              <table className="w-full table-fixed text-sm">
                <thead>
                  <tr className="border-b border-white/5 text-[10px] font-black uppercase tracking-widest text-slate-500">
                    <th className="text-left px-4 py-2.5">Symbol</th>
                    <th className="text-left px-3 py-2.5 w-28">Action</th>
                    <th className="text-right px-3 py-2.5 w-24">Value</th>
                    <th className="text-right px-3 py-2.5 w-24">Currency</th>
                    <th className="text-left px-3 py-2.5 w-36">Date</th>
                    <th className="px-2 py-2.5 w-9"></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {customTrades.map((t, i) => (
                    <tr key={i} className="bg-surface/30">
                      <td className="px-4 py-2">
                        <input
                          type="text"
                          value={t.symbol}
                          onChange={e => setCustomTrades(prev => prev.map((x, j) => j === i ? { ...x, symbol: e.target.value.toUpperCase() } : x))}
                          placeholder="Yahoo ticker"
                          title="Yahoo Finance ticker (e.g. AAPL, VUAA.L, CSPX.L)"
                          className="w-full bg-bg border border-border-dim rounded-lg px-2 py-1.5 text-slate-100 font-mono text-sm focus:outline-none focus:border-indigo-500/50"
                        />
                      </td>
                      <td className="px-3 py-2">
                        <SelectInput
                          options={['buy_amount', 'sell_qty']}
                          labels={['Buy $', 'Sell qty']}
                          value={t.action}
                          onChange={v => setCustomTrades(prev => prev.map((x, j) => j === i ? { ...x, action: v as 'buy_amount' | 'sell_qty' } : x))}
                        />
                      </td>
                      <td className="px-3 py-2 text-right">
                        <NumberInput
                          value={t.value.toString()}
                          onChange={v => setCustomTrades(prev => prev.map((x, j) => j === i ? { ...x, value: parseFloat(v) || 0 } : x))}
                          min={0}
                        />
                      </td>
                      <td className="px-3 py-2 text-right">
                        <SelectInput options={CURRENCIES} value={t.currency} onChange={v => setCustomTrades(prev => prev.map((x, j) => j === i ? { ...x, currency: v } : x))} />
                      </td>
                      <td className="px-3 py-2">
                        <DatePicker value={t.date} onChange={d => setCustomTrades(prev => prev.map((x, j) => j === i ? { ...x, date: d } : x))} />
                      </td>
                      <td className="px-2 py-2 text-center">
                        <button
                          onClick={() => setCustomTrades(prev => prev.filter((_, j) => j !== i))}
                          className="p-1.5 rounded-lg text-slate-500 hover:text-red-400 hover:bg-red-500/10 transition-colors"
                        >
                          <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                          </svg>
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <button
                onClick={() => setCustomTrades(prev => [...prev, { symbol: '', exchange: '', action: 'buy_amount', value: 0, currency: 'USD', date: today() }])}
                className="w-full px-4 py-2.5 text-sm text-slate-400 hover:text-white hover:bg-white/5 transition-colors flex items-center gap-2 border-t border-white/5"
              >
                <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
                </svg>
                Add custom trade
              </button>
            </div>
          </div>
        )}

        {/* ---- Custom Basket ---- */}
        {mode === 'basket' && (
          <div className="space-y-4">
            <p className="text-sm text-slate-400">
              Build a custom portfolio from scratch. Analytics will reflect performance of this basket from the acquisition date forward.
            </p>
            <BasketEditor
              items={basketItems}
              mode={basketMode}
              notional={notional}
              notionalCurrency={notionalCurrency}
              acquiredAt={acquiredAt}
              onItemsChange={setBasketItems}
              onModeChange={setBasketMode}
              onNotionalChange={setNotional}
              onNotionalCurrencyChange={setNotionalCurrency}
              onAcquiredAtChange={setAcquiredAt}
            />
          </div>
        )}

        {/* ---- Target Allocation (Redirect) ---- */}
        {mode === 'redirect' && (
          <div className="space-y-4">
            <p className="text-sm text-slate-400">
              Simulate investing all your historical cash flows into a target portfolio instead of your actual trades.
            </p>
            <BasketEditor
              items={redirectBasketItems}
              mode="weight"
              notional={0} // not used for display when mode="weight" without input, wait, notional input is shown.
              notionalCurrency={redirectNotionalCurrency}
              acquiredAt={today()} // Not used for redirect, but required by component. 
              onItemsChange={setRedirectBasketItems}
              onModeChange={() => {}} // locked to weight
              onNotionalChange={() => {}} // Not used
              onNotionalCurrencyChange={setRedirectNotionalCurrency}
              onAcquiredAtChange={() => {}} // Not used
              hideNotional={true}
              hideAcquiredAt={true}
            />
          </div>
        )}

        {/* ---- Historical Backtest ---- */}
        {mode === 'backtest' && (
          <div className="space-y-6">
            <p className="text-sm text-slate-400">
              Simulate a custom allocation over a historical period with optional contributions and rebalancing.
            </p>

            {/* Backtest params */}
            <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
              <div>
                <label className="block text-[9px] font-black text-slate-500 uppercase tracking-[0.2em] mb-1.5">Start Date</label>
                <DatePicker value={btStartDate} onChange={setBtStartDate} />
              </div>
              <div>
                <label className="block text-[9px] font-black text-slate-500 uppercase tracking-[0.2em] mb-1.5">Initial Amount</label>
                <NumberInput
                  value={btInitial.toString()}
                  onChange={v => setBtInitial(parseFloat(v) || 0)}
                  min={0}
                />
              </div>
              <div>
                <label className="block text-[9px] font-black text-slate-500 uppercase tracking-[0.2em] mb-1.5">Currency</label>
                <SelectInput
                  options={CURRENCIES}
                  value={btCurrency}
                  onChange={setBtCurrency}
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="flex flex-col gap-1.5">
                <label className="text-[9px] font-black text-slate-500 uppercase tracking-[0.2em]">Contribution</label>
                <div className="flex items-center gap-2">
                  <div className="flex-1">
                    <SelectInput
                      options={CONTRIBUTION_OPTIONS.map(o => o.value)}
                      value={btContribution}
                      onChange={v => setBtContribution(v as ContributionCadence)}
                    />
                  </div>
                  {btContribution !== 'none' && (
                    <div className="w-28">
                      <NumberInput
                        value={btContributionAmt.toString()}
                        onChange={v => setBtContributionAmt(parseFloat(v) || 0)}
                        placeholder="Amount"
                        min={0}
                      />
                    </div>
                  )}
                </div>
              </div>
              <div className="flex flex-col gap-1.5">
                <label className="text-[9px] font-black text-slate-500 uppercase tracking-[0.2em]">Rebalance</label>
                <div className="flex items-center gap-2">
                  <div className="flex-1">
                    <SelectInput
                      options={REBALANCE_OPTIONS.map(o => o.value)}
                      value={btRebalance}
                      onChange={v => setBtRebalance(v as RebalanceMode)}
                    />
                  </div>
                  {btRebalance === 'threshold' && (
                    <div className="flex items-center gap-1">
                      <div className="w-20">
                        <NumberInput
                          value={btThreshold.toString()}
                          onChange={v => setBtThreshold(parseFloat(v) || 0)}
                          min={0}
                          max={100}
                          step={0.5}
                        />
                      </div>
                      <span className="text-slate-400 text-sm">%</span>
                    </div>
                  )}
                </div>
              </div>
            </div>

            <div>
              <label className="block text-[9px] font-black text-slate-500 uppercase tracking-[0.2em] mb-3">Allocation</label>
              <BasketEditor
                items={btBasketItems}
                mode={btBasketMode}
                notional={btNotional}
                notionalCurrency={btNotionalCurrency}
                acquiredAt={btStartDate}
                onItemsChange={setBtBasketItems}
                onModeChange={setBtBasketMode}
                onNotionalChange={setBtNotional}
                onNotionalCurrencyChange={setBtNotionalCurrency}
                onAcquiredAtChange={() => {}}
              />
            </div>
          </div>
        )}

        {/* Footer actions */}
        {validationErrors.length > 0 && (
          <div className="rounded-xl border border-amber-500/30 bg-amber-500/5 px-4 py-3 space-y-1">
            {validationErrors.map((msg, i) => (
              <p key={i} className="text-amber-400 text-xs flex items-start gap-2">
                <svg className="w-3.5 h-3.5 mt-0.5 shrink-0" viewBox="0 0 20 20" fill="currentColor">
                  <path fillRule="evenodd" d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495zM10 5a.75.75 0 01.75.75v3.5a.75.75 0 01-1.5 0v-3.5A.75.75 0 0110 5zm0 9a1 1 0 100-2 1 1 0 000 2z" clipRule="evenodd" />
                </svg>
                {msg}
              </p>
            ))}
          </div>
        )}
        <div className="flex items-center gap-3 pt-2 border-t border-white/5">
          <button
            onClick={() => handleSave()}
            disabled={saving || validationErrors.length > 0}
            className="px-5 py-2.5 rounded-xl bg-indigo-600 hover:bg-indigo-500 text-white text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
          <button
            onClick={() => navigate(-1)}
            disabled={saving}
            className="px-5 py-2.5 rounded-xl text-slate-400 hover:text-white text-sm font-medium transition-colors"
          >
            Cancel
          </button>
        </div>
      </div>

      {pendingDelete && (
        <ConfirmDialog
          title="Delete scenario?"
          message="This cannot be undone."
          confirmLabel="Delete"
          busy={saving}
          onConfirm={doDelete}
          onCancel={() => setPendingDelete(false)}
        />
      )}
    </PageLayout>
  )
}
