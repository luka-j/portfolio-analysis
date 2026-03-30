# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

### Backend (run from `backend/`)
```bash
go run .                        # run the server
go build .                      # build binary
go test ./...                   # run all tests
go test ./services/flexquery/   # run tests in one package
go test -run TestName ./...     # run a single test by name
```

### Frontend (run from `frontend/`)
```bash
npm run dev      # dev server on :5173 (proxies /api to :8080)
npm run build    # tsc + vite build
npm run lint     # eslint
```

### Environment (backend)
Required env vars — see `backend/run.ps1` for a working local example:
- `DATABASE_URL` — PostgreSQL DSN
- `ALLOWED_TOKEN_HASHES` — comma-separated SHA-256 hashes of auth tokens; omit for open mode

Optional:
- `GEMINI_API_KEY` — enables LLM features (summary, chat). Models configured via `GEMINI_SUMMARY_MODEL` / `GEMINI_CHAT_MODEL`.
- `FUNDAMENTALS_PROVIDERS` / `BREAKDOWN_PROVIDERS` — comma-separated provider names (default: `"Yahoo"`)

## Architecture

### Backend
`main.go` wires everything together: it initialises the DB, builds all services, registers routes, and starts the background fundamentals fetcher. There are no DI frameworks — dependencies are passed explicitly.

**Request path:** `handlers/` → `services/` → DB or external API. Handlers are thin; business logic lives in services.

**Auth:** `middleware/TokenAuth` reads `X-Auth-Token`, hashes it with SHA-256, and checks against `AllowedTokenHashes`. If the list is empty the middleware is a no-op (open mode).

**Multi-user:** All portfolio data is scoped to a `User` row keyed by `TokenHash`. The `flexquery.Parser` resolves or creates the user on every upload/load.

**Database:** GORM with PostgreSQL (also supports SQLite via `glebarez/sqlite`). Schema is managed entirely by `db.AutoMigrate` on startup — no migration files. Tables: `users`, `transactions`, `market_data`, `asset_fundamentals`, `etf_breakdowns`, `llm_caches`.

**Key services:**
- `services/flexquery` — parses IB FlexQuery XML reports into `models.FlexQueryData`; persists to `transactions`. Deduplicates by IB's `tradeID`/`transactionID`. The parser resolves `assetCategory=STK + subCategory=ETF` → `"ETF"` at parse time.
- `services/portfolio` — reconstructs holdings and cost bases purely from transactions (no stored open-positions snapshot). Called on every request.
- `services/market` — Yahoo Finance price fetching + CNB FX rates; results cached in `market_data`.
- `services/fx` — currency conversion layer on top of market/CNB providers.
- `services/fundamentals` — background service (hourly cycle) that enriches `asset_fundamentals` and `etf_breakdowns`. Tier order: IB `assetCategory` → Yahoo `quoteType` → Yahoo fundamentals (country/sector) → Yahoo ETF breakdowns (pre-aggregated sector/country/bond-rating weights). Rate limits and cooldowns are tracked in-memory; all persistent state is in the DB. `upsertFundamentals` does **not** overwrite `asset_type` on conflict — classification comes from IB/Yahoo.
- `services/breakdown` — reads cached `asset_fundamentals` and `etf_breakdowns` to produce portfolio breakdown by type/asset/country/sector/bond-rating. For ETFs, uses Yahoo's pre-aggregated dimension weights (not individual holdings look-through). Makes no external calls.
- `services/stats` — portfolio analytics: TWR/MWR return calculations (`returns.go`), standalone risk metrics like Sharpe/Sortino/VAMI/max-drawdown (`standalone.go`), and benchmark-relative metrics like alpha/beta/tracking-error/information-ratio (`benchmark.go`). Benchmark metrics use `math/big` at 128-bit precision to avoid rounding errors on near-constant series. Alpha is annualised via compounding, not linear scaling.
- `services/llm` — Google Gemini integration for portfolio summaries and multi-turn chat. Caches responses in `llm_caches` table. Model selection (flash for summaries, pro for chat) via query param.
- `services/tax` — Czech tax report generation from transactions.
- `services/parsers` — eTrade benefit/gains CSV parsing (RSU/ESPP).

### Frontend
React 19 + TypeScript + Vite. API calls go through `src/api.ts`. `src/App.tsx` owns routing (React Router). Charts use Recharts. Styling is Tailwind CSS v4.

### Code conventions
- **Go:** No DI frameworks — explicit dependency passing. Wrap errors with `fmt.Errorf("context: %w", err)`. Every exported function has a one-line doc comment.
- **React:** Functional components using `export function Name() {}` (not arrow functions). No barrel files — import from specific modules. No `any` types. `usePersistentState` hook for localStorage-backed preferences (currency, date range, accounting model).
- **API contract:** JSON responses, `accounting_model=historical|spot|original` query param for FX mode, `X-Auth-Token` header for auth. Dates as ISO `YYYY-MM-DD`.

### Data flow for portfolio breakdown
1. FlexQuery upload → `transactions` rows with `asset_category = "ETF"` (for ETFs).
2. Fundamentals background cycle seeds `asset_fundamentals.asset_type = "ETF"` from the IB category, then fetches Yahoo fundamentals (country/sector) for stocks and Yahoo pre-aggregated breakdown weights (sector/country/bond-rating) for ETFs into `etf_breakdowns`.
3. `GET /portfolio/breakdown` → breakdown service reads `asset_fundamentals` for stocks and `etf_breakdowns` for ETFs; aggregates weighted by position value. Bond ETFs contribute to bond-rating dimension only; commodities are excluded from country/sector.
