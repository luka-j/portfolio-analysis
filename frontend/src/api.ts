const API_BASE = '/api/v1';

function getToken(): string {
  return localStorage.getItem('gofolio_token') || '';
}

export function setToken(token: string) {
  localStorage.setItem('gofolio_token', token);
}

export function clearToken() {
  localStorage.removeItem('gofolio_token');
}

export function hasToken(): boolean {
  return !!getToken();
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = getToken();
  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string> || {}),
  };
  if (token) {
    headers['X-Auth-Token'] = token;
  }

  const resp = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers,
  });

  if (resp.status === 401) {
    clearToken();
    window.location.href = '/login';
    throw new Error('Unauthorized');
  }

  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(body.error || resp.statusText);
  }

  return resp.json();
}

// ---------- Types ----------

export interface PositionValue {
  symbol: string;
  listing_exchange?: string;
  quantity: number;
  native_currency: string;
  yahoo_symbol?: string;
  prices: Record<string, number>;
  cost_bases: Record<string, number>;
  realized_gls: Record<string, number>;
  values: Record<string, number>;
  commissions: Record<string, number>;
}

export interface PortfolioValueResponse {
  values: Record<string, number>;
  positions: PositionValue[];
}

export interface DailyValue {
  date: string;
  value: number;
}

export interface PortfolioHistoryResponse {
  currency: string;
  accounting_model: string;
  data: DailyValue[];
}

export interface PricePoint {
  date: string;
  open: number;
  high: number;
  low: number;
  close: number;
  adj_close: number;
  volume: number;
}

export interface MarketHistoryResponse {
  symbol: string;
  data: PricePoint[];
}

export interface StatsResponse {
  currency: string;
  accounting_model: string;
  statistics: Record<string, unknown>;
}

export interface BenchmarkResult {
  symbol: string;
  alpha: number;
  beta: number;
  sharpe_ratio: number;
  treynor_ratio: number;
  tracking_error: number;
  information_ratio: number;
  correlation: number;
}

export interface CompareResponse {
  currency: string;
  accounting_model: string;
  benchmarks: BenchmarkResult[];
}

export interface UploadResponse {
  message: string;
  positions_count: number;
  trades_count: number;
  cash_transactions_count: number;
}

export interface TradeEntry {
  date: string;
  side: string;
  quantity: number;
  price: number;
  native_currency: string;
  converted_price: number;
  commission: number;
  proceeds: number;
}

export interface TradesResponse {
  symbol: string;
  currency: string;
  display_currency: string;
  trades: TradeEntry[];
}

// ---------- API Calls ----------

export async function uploadFlexQuery(file: File): Promise<UploadResponse> {
  const form = new FormData();
  form.append('file', file);
  return request<UploadResponse>('/portfolio/upload', {
    method: 'POST',
    body: form,
  });
}

export async function getPortfolioValue(currencies = 'USD,EUR,CZK', accountingModel = 'historical'): Promise<PortfolioValueResponse> {
  return request<PortfolioValueResponse>(`/portfolio/value?currencies=${currencies}&accounting_model=${accountingModel}`);
}

export async function getPortfolioHistory(
  from: string, to: string, currency: string, accountingModel = 'historical', signal?: AbortSignal
): Promise<PortfolioHistoryResponse> {
  return request<PortfolioHistoryResponse>(
    `/portfolio/history?from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}`,
    { signal }
  );
}

export async function getMarketHistory(
  symbol: string, from: string, to: string
): Promise<MarketHistoryResponse> {
  return request<MarketHistoryResponse>(
    `/market/history?symbol=${encodeURIComponent(symbol)}&from=${from}&to=${to}`
  );
}

export async function getPortfolioStats(
  from: string, to: string, currency: string, accountingModel = 'historical'
): Promise<StatsResponse> {
  return request<StatsResponse>(
    `/portfolio/stats?from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}`
  );
}

export async function comparePortfolio(
  symbols: string, currency: string, from: string, to: string,
  accountingModel = 'historical', riskFreeRate = 0.05
): Promise<CompareResponse> {
  return request<CompareResponse>(
    `/portfolio/compare?symbols=${encodeURIComponent(symbols)}&currency=${currency}&from=${from}&to=${to}&accounting_model=${accountingModel}&risk_free_rate=${riskFreeRate}`
  );
}

export async function getPortfolioTrades(
  symbol: string, currency = 'CZK', exchange = '', limit = 200, offset = 0
): Promise<TradesResponse & { total: number; limit: number; offset: number }> {
  const exchangeParam = exchange ? `&exchange=${encodeURIComponent(exchange)}` : '';
  return request(
    `/portfolio/trades?symbol=${encodeURIComponent(symbol)}&currency=${currency}${exchangeParam}&limit=${limit}&offset=${offset}`
  );
}

// getPortfolioReturns fetches the real cumulative TWR series (in %) from the backend.
// This is the correct data source for the TWR chart — each value is the chain-linked
// time-weighted return up to that date, with cash flows properly neutralised.
export async function getPortfolioReturns(
  from: string, to: string, currency: string, accountingModel = 'historical', signal?: AbortSignal
): Promise<PortfolioHistoryResponse> {
  return request<PortfolioHistoryResponse>(
    `/portfolio/history/returns?from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}`,
    { signal }
  );
}

// Verify token is valid by making a lightweight call.
export async function verifyToken(): Promise<boolean> {
  try {
    await request('/health');
    return true;
  } catch {
    return false;
  }
}

export const updateSymbolMapping = (symbol: string, yahooSymbol: string, exchange?: string) => {
  const query = exchange ? `?exchange=${encodeURIComponent(exchange)}` : '';
  return request<{ message: string }>(`/portfolio/symbols/${encodeURIComponent(symbol)}/mapping${query}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ yahoo_symbol: yahooSymbol }),
  });
};
