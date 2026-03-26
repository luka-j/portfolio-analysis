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
- `ALPHAVANTAGE_API_KEY` — ETF holdings data (AlphaVantage)
- `FMP_API_KEY` — fundamentals data (FinancialModelingPrep)
- `ALLOWED_TOKEN_HASHES` — comma-separated SHA-256 hashes of auth tokens; omit for open mode

Optional rate-limit overrides: `FMP_REQUESTS_PER_MINUTE` (default 10), `FMP_REQUESTS_PER_DAY` (default 250), `AV_REQUESTS_PER_MINUTE` (default 5), `AV_REQUESTS_PER_DAY` (default 25).

## Architecture

### Backend
`main.go` wires everything together: it initialises the DB, builds all services, registers routes, and starts the background fundamentals fetcher. There are no DI frameworks — dependencies are passed explicitly.

**Request path:** `handlers/` → `services/` → DB or external API. Handlers are thin; business logic lives in services.

**Auth:** `middleware/TokenAuth` reads `X-Auth-Token`, hashes it with SHA-256, and checks against `AllowedTokenHashes`. If the list is empty the middleware is a no-op (open mode).

**Multi-user:** All portfolio data is scoped to a `User` row keyed by `TokenHash`. The `flexquery.Parser` resolves or creates the user on every upload/load.

**Database:** GORM with PostgreSQL (also supports SQLite via `glebarez/sqlite`). Schema is managed entirely by `db.AutoMigrate` on startup — no migration files. Tables: `users`, `transactions`, `market_data`, `asset_fundamentals`, `fund_holdings`.

**Key services:**
- `services/flexquery` — parses IB FlexQuery XML reports into `models.FlexQueryData`; persists to `transactions`. Deduplicates by IB's `tradeID`/`transactionID`. The parser resolves `assetCategory=STK + subCategory=ETF` → `"ETF"` at parse time.
- `services/portfolio` — reconstructs holdings and cost bases purely from transactions (no stored open-positions snapshot). Called on every request.
- `services/market` — Yahoo Finance price fetching + CNB FX rates; results cached in `market_data`.
- `services/fx` — currency conversion layer on top of market/CNB providers.
- `services/fundamentals` — background service (hourly cycle) that enriches `asset_fundamentals` and `fund_holdings`. Tier order: IB `assetCategory` → Yahoo `quoteType` → FMP (country/sector/industry) → AlphaVantage (ETF holdings). Rate limits and cooldowns are tracked in-memory; all persistent state is in the DB. `upsertFundamentals` does **not** overwrite `asset_type` on conflict — classification comes from IB/Yahoo, not FMP.
- `services/breakdown` — reads cached `asset_fundamentals` and `fund_holdings` to produce portfolio breakdown by type/asset/country/sector/industry. ETFs are look-through: their value is distributed proportionally to constituents. Makes no external calls.
- `services/tax` — Czech tax report generation from transactions.
- `services/parsers` — eTrade benefit/gains CSV parsing (RSU/ESPP).

### Frontend
React 19 + TypeScript + Vite. API calls go through `src/api.ts`. `src/App.tsx` owns routing (React Router). Charts use Recharts. Styling is Tailwind CSS v4.

### Data flow for ETF look-through breakdown
1. FlexQuery upload → `transactions` rows with `asset_category = "ETF"` (for ETFs).
2. Fundamentals background cycle seeds `asset_fundamentals.asset_type = "ETF"` from the IB category.
3. AlphaVantage holdings fetch populates `fund_holdings` for confirmed ETFs.
4. `GET /portfolio/breakdown` → breakdown service distributes each ETF position's value across its `fund_holdings` constituents; looks up each constituent's `asset_fundamentals` for country/sector.
