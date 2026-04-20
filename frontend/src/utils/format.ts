export function formatCurrency(value: number, currency: string, nativeCurrency?: string): string {
  const cur = currency === 'Original' ? (nativeCurrency || 'USD') : currency
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency', currency: cur, minimumFractionDigits: 2, maximumFractionDigits: 2,
    }).format(value)
  } catch {
    return value.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + ' ' + cur
  }
}

export function formatCurrencyCompact(value: number, currency: string): string {
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency', currency, minimumFractionDigits: 0, maximumFractionDigits: 0,
    }).format(value)
  } catch {
    return `${currency} ${value.toFixed(0)}`
  }
}

export function formatNumber(value: number, decimals = 2): string {
  return value.toLocaleString('en-US', { minimumFractionDigits: decimals, maximumFractionDigits: decimals })
}

/** Formats a quantity with up to maxDecimals significant fractional digits, trimming trailing zeros. */
export function formatQuantity(value: number, maxDecimals = 2): string {
  return value.toLocaleString('en-US', { minimumFractionDigits: 0, maximumFractionDigits: maxDecimals })
}

export function escapeCSVField(value: string): string {
  if (value.includes(',') || value.includes('"') || value.includes('\n')) {
    return '"' + value.replace(/"/g, '""') + '"'
  }
  return value
}

export function formatDate(d: Date): string {
  return d.toISOString().slice(0, 10)
}

export const CURRENCIES = ['CZK', 'USD', 'EUR'] as const

/** Currency symbol map for quick display. */
export const CURRENCY_SYMBOLS: Record<string, string> = {
  CZK: 'Kč',
  USD: '$',
  EUR: '€',
}

/**
 * Returns the ISO start date for a rolling period.
 * months=0 means "all time" — returns a far-past sentinel date.
 */
export function getFromDate(months: number): string {
  if (months === 0) return '2000-01-01'
  const d = new Date()
  d.setMonth(d.getMonth() - months)
  return formatDate(d)
}

/** Shared Recharts Tooltip contentStyle — apply to every chart's <Tooltip contentStyle={…} />. */
export const RECHARTS_TOOLTIP_STYLE: Record<string, unknown> = {
  backgroundColor: 'rgba(26,29,46,0.98)',
  border: '1px solid rgba(99,102,241,0.3)',
  borderRadius: '12px',
  fontSize: '11px',
  color: '#e2e8f0',
  backdropFilter: 'blur(32px)',
  boxShadow: '0 25px 50px -12px rgba(0, 0, 0, 0.5)',
}

/** Shared Recharts Tooltip labelStyle. */
export const RECHARTS_LABEL_STYLE: Record<string, unknown> = {
  color: '#6366f1',
  marginBottom: '6px',
  fontSize: '9px',
  textTransform: 'uppercase',
  letterSpacing: '0.25em',
  fontWeight: '900',
  opacity: 0.8,
}

/** Shared Recharts Tooltip itemStyle — ensures readable text on dark backgrounds. */
export const RECHARTS_ITEM_STYLE: Record<string, unknown> = {
  color: '#e2e8f0',
  fontSize: '11px',
}
export const CURRENCIES_WITH_ORIGINAL = ['CZK', 'USD', 'EUR', 'Original'] as const
