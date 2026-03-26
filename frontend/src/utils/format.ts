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
export const CURRENCIES_WITH_ORIGINAL = ['CZK', 'USD', 'EUR', 'Original'] as const
