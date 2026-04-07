const API_BASE = '/api/v1';

function getToken(): string {
  return localStorage.getItem('portfolio_token') || '';
}

export function setToken(token: string) {
  localStorage.setItem('portfolio_token', token);
}

export function clearToken() {
  localStorage.removeItem('portfolio_token');
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
  price: number;
  cost_basis: number;
  realized_gl: number;
  value: number;
  commission: number;
  bond_duration?: number; // bond ETF: effective duration in years
  name?: string;          // security long name
}

export interface PortfolioValueResponse {
  value: number;
  currency: string;
  positions: PositionValue[];
  has_transactions: boolean;
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
  error?: string;
  alpha: number;
  beta: number;
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

export interface StandaloneResult {
  symbol: string;
  error?: string;
  sharpe_ratio: number;
  vami: number;
  volatility: number;
  sortino_ratio: number;
  max_drawdown: number;
}

export interface StandaloneResponse {
  currency: string;
  accounting_model: string;
  results: StandaloneResult[];
}

export interface UploadResponse {
  message: string;
  positions_count: number;
  trades_count: number;
  cash_transactions_count: number;
}

export interface EtradeUploadResponse {
  message: string;
  saved_count: number;
  parsed_count: number;
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
  tax_cost_basis?: number;
}

export interface TradesResponse {
  symbol: string;
  currency: string;
  display_currency: string;
  trades: TradeEntry[];
}

export interface TaxTransaction {
  type: string;
  symbol: string;
  date: string;
  quantity: number;
  native_price: number;
  currency: string;
  exchange_rate: number;
  cost_czk: number;
  benefit_czk: number;
  buy_date?: string;
  buy_rate?: number;
  buy_commission?: number;
  sell_commission?: number;
}

export interface TaxReportSection {
  total_cost_czk: number;
  total_benefit_czk: number;
  transactions: TaxTransaction[];
}

export interface TaxReportResponse {
  year: number;
  employment_income: TaxReportSection;
  investment_income: TaxReportSection;
}

// ---- LLM ----

export interface LLMSummaryResponse {
  summary: string;
}

export interface LLMChatRequest {
  prompt_type: string;
  message: string;
  currency: string;
  model?: 'flash' | 'pro';
  force_refresh?: boolean;
  // Freeform-only
  include_portfolio?: boolean;
  override_portfolio_weights?: { symbol: string; weight: number }[];
  history?: { role: 'user' | 'assistant'; content: string }[];
  // ticker_analysis
  symbol?: string;
  // risk_metrics and benchmark_analysis
  from?: string;
  to?: string;
  accounting_model?: string;
  risk_free_rate?: number;
  // benchmark_analysis
  benchmark_symbol?: string;
}

export interface LLMChatResponse {
  response: string;
  cached?: boolean;
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

export async function uploadEtradeBenefits(file: File): Promise<EtradeUploadResponse> {
  const form = new FormData();
  form.append('file', file);
  return request<EtradeUploadResponse>('/portfolio/upload/etrade/benefits', {
    method: 'POST',
    body: form,
  });
}

export async function uploadEtradeSales(file: File): Promise<EtradeUploadResponse> {
  const form = new FormData();
  form.append('file', file);
  return request<EtradeUploadResponse>('/portfolio/upload/etrade/sales', {
    method: 'POST',
    body: form,
  });
}

export async function getPortfolioValue(currency = 'USD', accountingModel = 'historical', cachedOnly = false): Promise<PortfolioValueResponse> {
  const query = `currencies=${encodeURIComponent(currency)}&accounting_model=${accountingModel}${cachedOnly ? '&cachedOnly=true' : ''}`;
  return request<PortfolioValueResponse>(`/portfolio/value?${query}`);
}

export async function getPortfolioHistory(
  from: string, to: string, currency: string, accountingModel = 'historical', cachedOnly = false, signal?: AbortSignal
): Promise<PortfolioHistoryResponse> {
  const query = `from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}${cachedOnly ? '&cachedOnly=true' : ''}`;
  return request<PortfolioHistoryResponse>(
    `/portfolio/history?${query}`,
    { signal }
  );
}

export async function getMarketHistory(
  symbol: string, from: string, to: string,
  currency = 'USD', accountingModel = 'historical'
): Promise<MarketHistoryResponse> {
  return request<MarketHistoryResponse>(
    `/market/history?symbol=${encodeURIComponent(symbol)}&from=${from}&to=${to}&currency=${encodeURIComponent(currency)}&accounting_model=${accountingModel}`
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

export async function getStandaloneMetrics(
  symbols: string, currency: string, from: string, to: string,
  accountingModel = 'historical', riskFreeRate = 0.05
): Promise<StandaloneResponse> {
  const symParam = symbols ? `&symbols=${encodeURIComponent(symbols)}` : ''
  return request<StandaloneResponse>(
    `/portfolio/standalone?currency=${currency}&from=${from}&to=${to}&accounting_model=${accountingModel}&risk_free_rate=${riskFreeRate}${symParam}`
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

// getPortfolioReturns fetches the real cumulative TWR or MWR series (in %) from the backend.
// This is the correct data source for the TWR / MWR chart — each value is the chain-linked
// return up to that date, with cash flows properly neutralised.
export async function getPortfolioReturns(
  from: string, to: string, currency: string, accountingModel = 'historical', returnType = 'twr', cachedOnly = false, signal?: AbortSignal
): Promise<PortfolioHistoryResponse> {
  const query = `from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}&type=${returnType}${cachedOnly ? '&cachedOnly=true' : ''}`;
  return request<PortfolioHistoryResponse>(
    `/portfolio/history/returns?${query}`,
    { signal }
  );
}

// Verify token is valid by making a lightweight call.
export async function verifyToken(): Promise<boolean> {
  try {
    await request('/auth/verify');
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

export async function getTaxReport(year: number, exchangeRates?: Record<string, number>): Promise<TaxReportResponse> {
  return request<TaxReportResponse>(`/tax/report`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ year, exchange_rates: exchangeRates }),
  });
}

// ---- Portfolio Breakdown ----

export interface BreakdownEntry {
  label: string;
  value: number;
  percentage: number;
}

export interface BreakdownSection {
  title: string;
  note?: string;
  entries: BreakdownEntry[];
}

export interface BreakdownResponse {
  currency: string;
  sections: BreakdownSection[];
}

export async function getPortfolioBreakdown(currency = 'USD', cachedOnly = false): Promise<BreakdownResponse> {
  return request<BreakdownResponse>(`/portfolio/breakdown?currency=${encodeURIComponent(currency)}${cachedOnly ? '&cachedOnly=true' : ''}`);
}

// ---- Portfolio Price History ----

export interface SymbolPriceHistory {
  symbol: string;
  exchange?: string;
  change_pct: number | null;
  avg_price: number | null;
  currency: string;
}

export async function getPortfolioPriceHistory(
  from: string, to: string, currency: string, accountingModel = 'historical'
): Promise<{ items: SymbolPriceHistory[] }> {
  return request<{ items: SymbolPriceHistory[] }>(
    `/portfolio/price-history?from=${from}&to=${to}&currency=${encodeURIComponent(currency)}&accounting_model=${accountingModel}`
  );
}

export async function getLLMAvailable(): Promise<{ available: boolean }> {
  return request<{ available: boolean }>('/llm/available');
}

export async function getLLMSummary(period = '1d', currency = 'USD', forceRefresh = false): Promise<LLMSummaryResponse> {
  const extra = forceRefresh ? '&force_refresh=true' : '';
  return request<LLMSummaryResponse>(`/llm/summary?period=${period}&currency=${encodeURIComponent(currency)}${extra}`);
}

export async function postLLMChat(req: LLMChatRequest): Promise<LLMChatResponse> {
  return request<LLMChatResponse>('/llm/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}
