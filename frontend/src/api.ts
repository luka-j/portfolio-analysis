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
    window.dispatchEvent(new CustomEvent('portfolio:unauthorized'));
    throw new Error('Unauthorized');
  }

  if (!resp.ok) {
    let errorMsg: string;
    try {
      const body = await resp.json();
      errorMsg = body.error || resp.statusText || `Request failed (${resp.status})`;
    } catch {
      // Non-JSON response (e.g. HTML error page from nginx)
      if (resp.status === 413) {
        errorMsg = 'File too large';
      } else {
        errorMsg = resp.statusText || `Request failed (${resp.status})`;
      }
    }
    throw new Error(errorMsg);
  }

  return resp.json();
}

function scenarioParam(id?: number | null): string {
  // 0 means "Real portfolio" (no param); null/undefined also means Real.
  return id != null && id > 0 ? `&scenario_id=${id}` : '';
}

// ---------- Types ----------

export interface PositionValue {
  symbol: string;
  listing_exchange?: string;
  quantity: number;
  native_currency: string;
  yahoo_symbol?: string;
  prices?: Record<string, number>;
  cost_bases?: Record<string, number>;
  values?: Record<string, number>;
  price: number;
  cost_basis: number;
  realized_gl: number;
  value: number;
  commission: number;
  bond_duration?: number;
  name?: string;
  isin?: string;
  asset_type?: string;
  price_status?: 'no_data' | 'stale' | 'fetch_failed';
}

export interface PortfolioValueResponse {
  value: number;
  currency: string;
  positions: PositionValue[];
  has_transactions: boolean;
  pending_cash?: number;
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
  statistics: Record<string, number>;
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

export interface ImportedTransaction {
  id: string;
  symbol: string;
  date: string; // YYYY-MM-DD
  side: string; // BUY, SELL, ESPP_VEST, RSU_VEST, etc.
  quantity: number;
  price: number;
  currency: string;
  total_cost: number; // abs(quantity * price)
  is_duplicate: boolean;
  confident_dedup: boolean;
  suspected_duplicate_id?: string; // PublicID of a manual entry that matches this new row
}

export interface ImportedCorporateAction {
  action_id: string;
  type: string; // IC, FS, RS, SD, CD
  symbol: string;
  new_symbol?: string;
  date: string; // YYYY-MM-DD
  description: string;
  split_ratio?: number;
  quantity?: number;
  amount?: number;
  currency?: string;
  is_new: boolean;
}

export interface UploadResponse {
  message: string;
  positions_count: number;
  trades_count: number;
  cash_transactions_count: number;
  corporate_actions_count?: number;
  transactions: ImportedTransaction[];
  corporate_actions?: ImportedCorporateAction[];
}

export interface EtradeUploadResponse {
  message: string;
  saved_count: number;
  parsed_count: number;
  transactions: ImportedTransaction[];
}

export interface TradeEntry {
  id: string;              // UUID from Transaction.PublicID
  entry_method?: string;   // "manual" | "flexquery" | "etrade_benefits" | "etrade_sales"
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

export interface AddTransactionRequest {
  transaction_type: 'buy' | 'sell' | 'espp_vest' | 'rsu_vest';
  symbol: string;
  currency: string;
  listing_exchange: string;
  date: string;       // YYYY-MM-DD
  quantity: number;
  price: number;
  commission?: number;
  tax_cost_basis?: number;
  force?: boolean;
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
  scenario_id?: number | null;
  // Freeform-only
  enabled_tools?: string[];
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
  // long_market_summary
  period?: string;
}

export interface LLMResponseSection {
  key: string;
  title: string;
  content: string;
}

export interface LLMToolCallEvent {
  tool: string;
  label: string;
}

export interface LLMChatResponse {
  response: string;
  cached?: boolean;
  sections?: LLMResponseSection[];
}

// ---- Scenario types ----

export type BaseMode = 'real' | 'empty';
export type AdjustmentAction = 'sell_qty' | 'sell_pct' | 'sell_all' | 'buy';
export type BasketMode = 'quantity' | 'weight';
export type RebalanceMode = 'none' | 'monthly' | 'quarterly' | 'annually' | 'threshold';
export type ContributionCadence = 'none' | 'monthly' | 'quarterly' | 'annually';

export interface Adjustment {
  symbol: string;
  action: AdjustmentAction;
  value: number;
  date?: string;       // YYYY-MM-DD
  currency?: string;
}

export interface BasketItem {
  symbol: string;
  quantity?: number;
  weight?: number;
  cost_basis?: number;
  currency: string;
}

export interface Basket {
  mode: BasketMode;
  items: BasketItem[];
  notional_value?: number;
  notional_currency?: string;
  acquired_at?: string;  // YYYY-MM-DD
}

export interface BacktestConfig {
  start_date: string;  // YYYY-MM-DD
  initial_amount: number;
  currency: string;
  contribution: ContributionCadence;
  contribution_amount: number;
  rebalance: RebalanceMode;
  rebalance_threshold: number;
}

export interface ScenarioSpec {
  base: BaseMode;
  base_as_of?: string;      // YYYY-MM-DD
  adjustments?: Adjustment[];
  basket?: Basket;
  backtest?: BacktestConfig;
}

export interface ScenarioSummary {
  id: number;
  name: string;
  pinned: boolean;
  created_at: string;
  last_used_at: string;
}

export interface ScenarioDetail extends ScenarioSummary {
  spec: ScenarioSpec;
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

export async function getPortfolioValue(
  currency = 'USD', accountingModel = 'historical', cachedOnly = false, scenarioId?: number | null
): Promise<PortfolioValueResponse> {
  const query = `currencies=${encodeURIComponent(currency)}&accounting_model=${accountingModel}${cachedOnly ? '&cachedOnly=true' : ''}${scenarioParam(scenarioId)}`;
  return request<PortfolioValueResponse>(`/portfolio/value?${query}`);
}

// getPortfolioValueMulti requests multiple currencies in a single backend pass:
// market data is fetched once and FX conversions happen locally in parallel.
// The primary currency's scalars (price/value/cost_basis) are still populated,
// while per-currency maps are filled for every requested currency.
export async function getPortfolioValueMulti(
  currencies: string[], accountingModel = 'historical', cachedOnly = false, signal?: AbortSignal, scenarioId?: number | null
): Promise<PortfolioValueResponse> {
  const query = `currencies=${currencies.map(encodeURIComponent).join(',')}&accounting_model=${accountingModel}${cachedOnly ? '&cachedOnly=true' : ''}${scenarioParam(scenarioId)}`;
  return request<PortfolioValueResponse>(`/portfolio/value?${query}`, { signal });
}

export async function getPortfolioHistory(
  from: string, to: string, currency: string, accountingModel = 'historical', cachedOnly = false, signal?: AbortSignal, scenarioId?: number | null
): Promise<PortfolioHistoryResponse> {
  const query = `from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}${cachedOnly ? '&cachedOnly=true' : ''}${scenarioParam(scenarioId)}`;
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
  from: string, to: string, currency: string, accountingModel = 'historical', cachedOnly = false, signal?: AbortSignal, scenarioId?: number | null
): Promise<StatsResponse> {
  return request<StatsResponse>(
    `/portfolio/stats?from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}${cachedOnly ? '&cachedOnly=true' : ''}${scenarioParam(scenarioId)}`,
    { signal }
  );
}

export async function comparePortfolio(
  symbols: string, currency: string, from: string, to: string,
  accountingModel = 'historical', riskFreeRate = 0.05, scenarioId?: number | null
): Promise<CompareResponse> {
  return request<CompareResponse>(
    `/portfolio/compare?symbols=${encodeURIComponent(symbols)}&currency=${currency}&from=${from}&to=${to}&accounting_model=${accountingModel}&risk_free_rate=${riskFreeRate}${scenarioParam(scenarioId)}`
  );
}

export async function getStandaloneMetrics(
  symbols: string, currency: string, from: string, to: string,
  accountingModel = 'historical', riskFreeRate = 0.05, cachedOnly = false, scenarioId?: number | null
): Promise<StandaloneResponse> {
  const symParam = symbols ? `&symbols=${encodeURIComponent(symbols)}` : ''
  const cachedParam = cachedOnly ? '&cachedOnly=true' : ''
  return request<StandaloneResponse>(
    `/portfolio/standalone?currency=${currency}&from=${from}&to=${to}&accounting_model=${accountingModel}&risk_free_rate=${riskFreeRate}${symParam}${cachedParam}${scenarioParam(scenarioId)}`
  );
}

export async function getPortfolioTrades(
  symbol: string, currency = 'CZK', exchange = '', limit = 200, offset = 0,
  accountingModel = 'historical'
): Promise<TradesResponse & { total: number; limit: number; offset: number }> {
  const exchangeParam = exchange ? `&exchange=${encodeURIComponent(exchange)}` : '';
  return request(
    `/portfolio/trades?symbol=${encodeURIComponent(symbol)}&currency=${encodeURIComponent(currency)}&accounting_model=${accountingModel}${exchangeParam}&limit=${limit}&offset=${offset}`
  );
}

// getPortfolioReturns fetches the real cumulative TWR or MWR series (in %) from the backend.
// This is the correct data source for the TWR / MWR chart — each value is the chain-linked
// return up to that date, with cash flows properly neutralised.
export async function getPortfolioReturns(
  from: string, to: string, currency: string, accountingModel = 'historical', returnType = 'twr', cachedOnly = false, signal?: AbortSignal, scenarioId?: number | null
): Promise<PortfolioHistoryResponse> {
  const query = `from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}&type=${returnType}${cachedOnly ? '&cachedOnly=true' : ''}${scenarioParam(scenarioId)}`;
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

export interface UpdateAssetRequest {
  name?: string
  asset_type?: string
  country?: string
  sector?: string
  yahoo_symbol?: string
  listing_exchange?: string
}

export const updateAsset = (symbol: string, exchange: string | undefined, req: UpdateAssetRequest) =>
  request<{ message: string }>(
    `/portfolio/assets/${encodeURIComponent(symbol)}${exchange ? `?exchange=${encodeURIComponent(exchange)}` : ''}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    }
  );

export const updateSymbolMapping = (symbol: string, yahooSymbol: string, exchange?: string) => {
  const query = exchange ? `?exchange=${encodeURIComponent(exchange)}` : '';
  return request<{ message: string }>(`/portfolio/symbols/${encodeURIComponent(symbol)}/mapping${query}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ yahoo_symbol: yahooSymbol }),
  });
};

export async function getTaxReport(
  year: number, exchangeRates?: Record<string, number>, scenarioId?: number | null
): Promise<TaxReportResponse> {
  const sid = scenarioParam(scenarioId)
  return request<TaxReportResponse>(`/tax/report${sid ? `?${sid.slice(1)}` : ''}`, {
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

export async function getPortfolioBreakdown(
  currency = 'USD', cachedOnly = false, scenarioId?: number | null
): Promise<BreakdownResponse> {
  return request<BreakdownResponse>(`/portfolio/breakdown?currency=${encodeURIComponent(currency)}${cachedOnly ? '&cachedOnly=true' : ''}${scenarioParam(scenarioId)}`);
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
  from: string, to: string, currency: string, accountingModel = 'historical', scenarioId?: number | null
): Promise<{ items: SymbolPriceHistory[] }> {
  return request<{ items: SymbolPriceHistory[] }>(
    `/portfolio/price-history?from=${from}&to=${to}&currency=${encodeURIComponent(currency)}&accounting_model=${accountingModel}${scenarioParam(scenarioId)}`
  );
}

export async function getLLMAvailable(): Promise<{ available: boolean; canned_model?: 'flash' | 'pro' }> {
  return request<{ available: boolean; canned_model?: 'flash' | 'pro' }>('/llm/available');
}

export async function getLLMSummary(period = '1d', forceRefresh = false, scenarioId?: number | null): Promise<LLMSummaryResponse> {
  const extra = forceRefresh ? '&force_refresh=true' : '';
  return request<LLMSummaryResponse>(`/llm/summary?period=${period}${extra}${scenarioParam(scenarioId)}`);
}

export async function postLLMChat(
  req: LLMChatRequest,
  onChunk?: (text: string) => void,
  onToolCall?: (event: LLMToolCallEvent) => void,
): Promise<LLMChatResponse> {
  const token = getToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  if (token) headers['X-Auth-Token'] = token;

  // scenario_id rides the URL so the backend ScenarioMiddleware (c.Query) picks it up for both
  // GET and POST handlers without needing body parsing.
  const sid = req.scenario_id != null && req.scenario_id > 0 ? `?scenario_id=${req.scenario_id}` : '';
  const resp = await fetch(`${API_BASE}/llm/chat${sid}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(req),
  });

  if (resp.status === 401) {
    clearToken();
    window.dispatchEvent(new CustomEvent('portfolio:unauthorized'));
    throw new Error('Unauthorized');
  }

  if (!resp.ok) {
    let errorMsg = `Request failed (${resp.status})`;
    try {
      const errorText = await resp.text();
      try {
        const body = JSON.parse(errorText);
        errorMsg = body.error || errorMsg;
      } catch {
        errorMsg = errorText || errorMsg;
      }
    } catch { /* use existing errorMsg */ }
    throw new Error(errorMsg);
  }

  const reader = resp.body?.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let fullResponse: LLMChatResponse = { response: '' };
  let streamedContent = '';

  if (!reader) return fullResponse;

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const parts = buffer.split('\n\n');
    buffer = parts.pop() || '';

    for (const part of parts) {
      if (!part.trim()) continue;

      const lines = part.split('\n');
      let eventType = 'message';
      let dataStr = '';

      for (const line of lines) {
        if (line.startsWith('event:')) {
          eventType = line.substring(6).trim();
        } else if (line.startsWith('data:')) {
          dataStr += line.substring(5).trim();
        }
      }

      if (dataStr) {
        try {
          const data = JSON.parse(dataStr);
          if (eventType === 'error') {
            throw new Error(data.error);
          } else if (eventType === 'tool_call') {
            if (onToolCall) onToolCall(data as LLMToolCallEvent);
          } else if (eventType === 'chunk') {
            streamedContent += data;
            if (onChunk) onChunk(streamedContent);
          } else if (eventType === 'message' || eventType === 'done') {
            fullResponse = data;
            if (eventType === 'message' && data.response && onChunk) {
              onChunk(data.response);
            }
          }
        } catch (e) {
          if (e instanceof Error && !e.message.startsWith('Unexpected') && eventType === 'error') {
            throw e;
          }
        }
      }
    }
  }

  return fullResponse;
}

// ---- Manual Transaction Entry ----

export async function addTransaction(req: AddTransactionRequest): Promise<{ status: string; id: string }> {
  return request<{ status: string; id: string }>('/portfolio/transactions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

export async function deleteTransaction(id: string): Promise<void> {
  await request<void>(`/portfolio/transactions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

// ---- Security Price Chart ----

export interface SecurityChartPoint {
  date: string;       // "YYYY-MM-DD"
  close: number;
  ma: number | null;  // null until the MA window fills
}

export interface SecurityChartResponse {
  symbol: string;
  from: string;
  to: string;
  ma_days: number;
  data: SecurityChartPoint[];
}

export async function getSecurityChart(
  symbol: string,
  from: string,    // YYYY-MM-DD, computed by the frontend from the period selector
  to: string,      // YYYY-MM-DD, today
  maDays: number,
  signal?: AbortSignal,
  currency?: string,       // omit → native currency (no conversion)
  accountingModel?: string, // pass 'original' to keep native prices when currency is set
): Promise<SecurityChartResponse> {
  // Send accounting_model alongside currency. accounting_model=original tells the
  // backend to skip FX conversion regardless of the currency value.
  const currencyParam = currency
    ? `&currency=${encodeURIComponent(currency)}&accounting_model=${accountingModel ?? 'historical'}`
    : ''
  return request<SecurityChartResponse>(
    `/market/security-chart?symbol=${encodeURIComponent(symbol)}&from=${from}&to=${to}&ma_days=${maDays}${currencyParam}`,
    { signal },
  );
}

// ---- Drawdown ----

export interface DrawdownPoint {
  date: string;
  drawdown_pct: number;
  peak: number;
  wealth: number;
}

export interface DrawdownResult {
  symbol: string;
  error?: string;
  series: DrawdownPoint[];
}

export interface DrawdownResponse {
  currency: string;
  accounting_model: string;
  series: DrawdownPoint[];    // backward-compat: portfolio-only
  results: DrawdownResult[];  // portfolio + optional benchmarks
}

export async function getDrawdownSeries(
  from: string, to: string, currency: string, accountingModel = 'historical', cachedOnly = false,
  symbols?: string, scenarioId?: number | null,
): Promise<DrawdownResponse> {
  const symParam = symbols ? `&symbols=${encodeURIComponent(symbols)}` : '';
  return request<DrawdownResponse>(
    `/portfolio/drawdown?from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}${cachedOnly ? '&cachedOnly=true' : ''}${symParam}${scenarioParam(scenarioId)}`
  );
}

// ---- Rolling Metrics ----

export interface RollingPoint {
  date: string;
  value: number;
}

export interface RollingSeriesResult {
  symbol: string;
  error?: string;
  series: RollingPoint[];
}

export interface RollingResponse {
  currency: string;
  accounting_model: string;
  metric: string;
  window: number;
  results: RollingSeriesResult[];
}

export async function getRollingMetric(
  metric: 'sharpe' | 'volatility' | 'beta' | 'sortino',
  window: number,
  from: string, to: string, currency: string,
  accountingModel = 'historical',
  riskFreeRate = 0.05,
  benchmark?: string,
  symbols?: string,
  scenarioId?: number | null,
): Promise<RollingResponse> {
  const benchParam = benchmark ? `&benchmark=${encodeURIComponent(benchmark)}` : '';
  const symParam = symbols ? `&symbols=${encodeURIComponent(symbols)}` : '';
  return request<RollingResponse>(
    `/portfolio/rolling?metric=${metric}&window=${window}&from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}&risk_free_rate=${riskFreeRate}${benchParam}${symParam}${scenarioParam(scenarioId)}`
  );
}

// ---- Attribution ----

export interface AttributionResult {
  symbol: string;
  avg_weight: number;
  return: number;
  contribution: number;
}

export interface AttributionResponse {
  currency: string;
  accounting_model: string;
  total_twr: number;
  positions: AttributionResult[];
}

export async function getAttribution(
  from: string, to: string, currency: string, accountingModel = 'historical', riskFreeRate = 0.05, scenarioId?: number | null
): Promise<AttributionResponse> {
  return request<AttributionResponse>(
    `/portfolio/attribution?from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}&risk_free_rate=${riskFreeRate}${scenarioParam(scenarioId)}`
  );
}

// ---- Correlation Matrix ----

export interface CorrelationMatrixResponse {
  currency: string;
  accounting_model: string;
  symbols: string[];
  matrix: number[][];
}

export async function getCorrelations(
  from: string, to: string, currency: string, accountingModel = 'historical', scenarioId?: number | null
): Promise<CorrelationMatrixResponse> {
  return request<CorrelationMatrixResponse>(
    `/portfolio/correlations?from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}${scenarioParam(scenarioId)}`
  );
}

// ---- Cumulative Return Series ----

export interface CumulativePoint {
  date: string;
  value: number; // cumulative return in percent
}

export interface CumulativeSeriesResult {
  symbol: string;
  error?: string;
  series: CumulativePoint[];
}

export interface CumulativeResponse {
  currency: string;
  accounting_model: string;
  results: CumulativeSeriesResult[];
}

export async function getCumulativeSeries(
  from: string, to: string, currency: string, accountingModel = 'historical', cachedOnly = false,
  symbols?: string, scenarioId?: number | null,
): Promise<CumulativeResponse> {
  const symParam = symbols ? `&symbols=${encodeURIComponent(symbols)}` : '';
  const cachedParam = cachedOnly ? '&cachedOnly=true' : '';
  return request<CumulativeResponse>(
    `/portfolio/cumulative?from=${from}&to=${to}&currency=${currency}&accounting_model=${accountingModel}${cachedParam}${symParam}${scenarioParam(scenarioId)}`
  );
}

// ---- Scenarios ----

export async function listScenarios(): Promise<ScenarioSummary[]> {
  return request<ScenarioSummary[]>('/scenarios');
}

export async function getScenario(id: number): Promise<ScenarioDetail> {
  return request<ScenarioDetail>(`/scenarios/${id}`);
}

export async function createScenario(spec: ScenarioSpec, name?: string, pinned = false): Promise<ScenarioDetail> {
  return request<ScenarioDetail>('/scenarios', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ spec, name: name ?? '', pinned }),
  });
}

export async function updateScenario(
  id: number,
  patch: { name?: string; pinned?: boolean; spec?: ScenarioSpec }
): Promise<ScenarioDetail> {
  return request<ScenarioDetail>(`/scenarios/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  });
}

export async function deleteScenario(id: number): Promise<void> {
  await request<void>(`/scenarios/${id}`, { method: 'DELETE' });
}

export interface CompareScenariosLLMResponse {
  response: string;
  cached: boolean;
}

export async function compareScenariosLLM(
  aId: number, bId: number, question?: string, currency = 'USD'
): Promise<CompareScenariosLLMResponse> {
  return request<CompareScenariosLLMResponse>('/scenarios/compare-llm', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ a_id: aId, b_id: bId, question, currency }),
  });
}
