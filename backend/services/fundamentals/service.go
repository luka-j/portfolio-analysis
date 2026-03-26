package fundamentals

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gofolio-analysis/models"
)

// QuoteTypeFetcher resolves a symbol's asset class using a free price API (no premium quota).
// Implemented by *market.YahooFinanceService.
type QuoteTypeFetcher interface {
	GetQuoteType(symbol string) (string, error)
}

// perProviderState tracks request counters and cooldown timers for one external provider.
// Counters reset to zero on restart (conservative: a restart is treated as a new window).
type perProviderState struct {
	mu            sync.Mutex
	minuteCount   int
	dayCount      int
	minuteReset   time.Time
	dayReset      time.Time
	cooldownUntil time.Time
}

// available returns true when the provider has remaining quota and is not in cooldown.
func (s *perProviderState) available(cfg RateLimitConfig) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Before(s.cooldownUntil) {
		return false
	}
	s.resetIfNeeded(now)
	return (cfg.RequestsPerMinute <= 0 || s.minuteCount < cfg.RequestsPerMinute) &&
		(cfg.RequestsPerDay <= 0 || s.dayCount < cfg.RequestsPerDay)
}

// consume records one successful request. Must be called only when available() was true.
func (s *perProviderState) consume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.minuteCount++
	s.dayCount++
}

// triggerCooldown puts the provider into cooldown for cfg.CooldownDuration.
func (s *perProviderState) triggerCooldown(cfg RateLimitConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cooldownUntil = time.Now().Add(cfg.CooldownDuration)
}

// triggerDailyCooldown puts the provider into cooldown until the start of the next UTC day.
// Use this when the provider signals its daily quota is exhausted.
func (s *perProviderState) triggerDailyCooldown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	s.cooldownUntil = tomorrow
	// Pin the day counter at max so resetIfNeeded doesn't clear it before midnight.
	s.dayCount = int(^uint(0) >> 1)
}

func (s *perProviderState) resetIfNeeded(now time.Time) {
	if now.After(s.minuteReset) {
		s.minuteCount = 0
		s.minuteReset = now.Add(time.Minute)
	}
	if now.After(s.dayReset) {
		s.dayCount = 0
		s.dayReset = now.Add(24 * time.Hour)
	}
}

// Service orchestrates periodic fetching of asset fundamentals and ETF breakdown data.
// All persistent state lives in the database; in-memory state is only rate-limit
// counters (which safely reset to zero on restart).
type Service struct {
	DB                    *gorm.DB
	QuoteTypeFetcher      QuoteTypeFetcher // optional; if nil, Yahoo bootstrap step is skipped
	fundamentalsProviders []FundamentalsProvider
	breakdownProviders    []ETFBreakdownProvider
	fundamentalsStates    map[string]*perProviderState
	breakdownStates       map[string]*perProviderState
	triggerCh             chan struct{}
}

// NewService constructs a Service from ordered provider slices (index 0 = highest priority).
func NewService(
	db *gorm.DB,
	funProviders []FundamentalsProvider,
	bdProviders []ETFBreakdownProvider,
	qtFetcher QuoteTypeFetcher,
) *Service {
	fStates := make(map[string]*perProviderState, len(funProviders))
	for _, p := range funProviders {
		fStates[p.Name()] = &perProviderState{
			minuteReset: time.Now().Add(time.Minute),
			dayReset:    time.Now().Add(24 * time.Hour),
		}
	}
	bStates := make(map[string]*perProviderState, len(bdProviders))
	for _, p := range bdProviders {
		bStates[p.Name()] = &perProviderState{
			minuteReset: time.Now().Add(time.Minute),
			dayReset:    time.Now().Add(24 * time.Hour),
		}
	}
	return &Service{
		DB:                    db,
		QuoteTypeFetcher:      qtFetcher,
		fundamentalsProviders: funProviders,
		breakdownProviders:    bdProviders,
		fundamentalsStates:    fStates,
		breakdownStates:       bStates,
		triggerCh:             make(chan struct{}, 1),
	}
}

// BuildFromConfig constructs a Service from comma-separated provider name lists and provider maps.
func BuildFromConfig(
	db *gorm.DB,
	fundamentalsOrder string,
	breakdownOrder string,
	allFundamentals map[string]FundamentalsProvider,
	allBreakdowns map[string]ETFBreakdownProvider,
	qtFetcher QuoteTypeFetcher,
) *Service {
	var funProviders []FundamentalsProvider
	for _, name := range splitNames(fundamentalsOrder) {
		if p, ok := allFundamentals[name]; ok {
			funProviders = append(funProviders, p)
		}
	}
	var bdProviders []ETFBreakdownProvider
	for _, name := range splitNames(breakdownOrder) {
		if p, ok := allBreakdowns[name]; ok {
			bdProviders = append(bdProviders, p)
		}
	}
	return NewService(db, funProviders, bdProviders, qtFetcher)
}

func splitNames(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// StartBackgroundFetcher launches the periodic fetch loop. Call once at startup.
func (s *Service) StartBackgroundFetcher(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		s.runFetchCycle(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runFetchCycle(ctx)
			case <-s.triggerCh:
				s.runFetchCycle(ctx)
			}
		}
	}()
}

// TriggerFetch enqueues an immediate fetch cycle (non-blocking).
func (s *Service) TriggerFetch() {
	select {
	case s.triggerCh <- struct{}{}:
	default:
	}
}

// runFetchCycle executes one full data-enrichment pass in strict tier order:
//
//  1. Bootstrap AssetType from the IB broker statement (already in DB, free).
//  2. Fill remaining unknowns from Yahoo Finance quoteType (free, same API).
//  3. FMP — country / sector for symbols still incomplete or stale.
//  4. Yahoo quoteSummary — aggregate sector/country breakdown for confirmed ETFs.
//
// All queues are built from the database each run — safe across any restart.
func (s *Service) runFetchCycle(ctx context.Context) {
	log.Println("fundamentals: starting fetch cycle")
	today := todayUTC()

	// Step 1 & 2: collect all relevant symbols, then bootstrap AssetType from free sources.
	allSymbols, err := s.collectAllSymbols()
	if err != nil {
		log.Printf("fundamentals: collect symbols: %v", err)
		return
	}
	s.bootstrapAssetTypes(ctx, allSymbols)

	// Step 3: FMP enrichment (country / sector) for stale or incomplete records.
	funQueue := s.buildFundamentalsQueue(allSymbols, today)
	if len(funQueue) > 0 {
		log.Printf("fundamentals: queueing %d symbols for fundamentals enrichment", len(funQueue))
	} else {
		log.Println("fundamentals: no symbols need fundamentals enrichment (all fresh or definitive)")
	}

	for _, sym := range funQueue {
		if ctx.Err() != nil {
			return
		}
		s.fetchOneFundamentals(sym)
	}

	// Step 4: Yahoo quoteSummary — aggregate breakdown for confirmed ETFs not updated today.
	bdQueue := s.buildBreakdownQueue(allSymbols, today)
	if len(bdQueue) > 0 {
		log.Printf("fundamentals: queueing %d ETFs for breakdown enrichment", len(bdQueue))
	} else {
		log.Println("fundamentals: no ETFs need breakdown enrichment")
	}

	for _, sym := range bdQueue {
		if ctx.Err() != nil {
			return
		}
		s.fetchOneBreakdown(sym)
	}

	log.Println("fundamentals: fetch cycle complete")
}

// ── Symbol collection ──────────────────────────────────────────────────────────

// collectAllSymbols returns portfolio-level symbols that have been priced by Yahoo.
// Comes from the database: the function is restart-safe.
func (s *Service) collectAllSymbols() ([]string, error) {
	return s.collectPortfolioSymbols()
}

// collectPortfolioSymbols returns effective ticker symbols for all portfolio positions
// that are safe to query externally (YahooSymbol set, or market_data row exists).
func (s *Service) collectPortfolioSymbols() ([]string, error) {
	type row struct {
		Symbol      string
		YahooSymbol string
	}
	var rows []row
	err := s.DB.Model(&models.Transaction{}).
		Select("DISTINCT symbol, yahoo_symbol").
		Where("type IN ?", []string{"Trade", "ESPP_VEST", "RSU_VEST"}).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("collect portfolio symbols: %w", err)
	}

	seen := make(map[string]struct{})
	var out []string
	for _, r := range rows {
		eff := effectiveSymbol(r.Symbol, r.YahooSymbol)
		if _, ok := seen[eff]; ok {
			continue
		}
		seen[eff] = struct{}{}

		if r.YahooSymbol != "" {
			out = append(out, eff) // user explicitly mapped — always include
			continue
		}
		if s.hasMarketData(eff) {
			out = append(out, eff) // Yahoo can resolve this ticker
		} else {
			log.Printf("fundamentals: skipping %q — no YahooSymbol and no market data", eff)
		}
	}
	return out, nil
}

// effectiveSymbol returns YahooSymbol when non-empty, otherwise the broker symbol.
func effectiveSymbol(brokerSym, yahooSym string) string {
	if yahooSym != "" {
		return yahooSym
	}
	return brokerSym
}

// hasMarketData returns true when at least one price row exists for symbol.
func (s *Service) hasMarketData(symbol string) bool {
	var count int64
	s.DB.Model(&models.MarketData{}).Where("symbol = ?", symbol).Count(&count)
	return count > 0
}

// ── Tier 0 + 0.5: Bootstrap AssetType from free sources ──────────────────────

// bootstrapAssetTypes seeds AssetType for every symbol using only free data:
//  1. IB broker AssetCategory from the transactions table (already in DB).
//  2. Yahoo Finance quoteType via a minimal 1-day chart request.
//
// FMP is NOT called here. Symbols that remain unknown after
// both free sources are left for FMP in buildFundamentalsQueue.
func (s *Service) bootstrapAssetTypes(ctx context.Context, symbols []string) {
	ibCount, yahooCount, skipCount := 0, 0, 0
	for _, sym := range symbols {
		if ctx.Err() != nil {
			return
		}

		// Skip if already definitively classified by IB itself.
		// If the type was set by a different source (FMP, Yahoo, AV), let IB re-confirm —
		// those sources can misclassify (e.g. FMP marks ETFs as Stock).
		var rec models.AssetFundamental
		if err := s.DB.Where("symbol = ?", sym).First(&rec).Error; err == nil {
			if isDefinitiveAssetType(rec.AssetType) && rec.DataSource == "IB" {
				skipCount++
				continue
			}
		}

		// Tier 0: IB broker category — definitive for ETF, Commodity, Bond; ambiguous for STK.
		if ibType := s.ibAssetType(sym); ibType != "" {
			s.seedAssetType(sym, ibType, "IB")
			ibCount++
			continue
		}

		// Tier 0.5: Yahoo Finance quoteType — free, already made for pricing anyway.
		if s.QuoteTypeFetcher != nil {
			qt, err := s.QuoteTypeFetcher.GetQuoteType(sym)
			if err != nil {
				log.Printf("fundamentals: Yahoo quoteType %s error: %v", sym, err)
			} else if qt != "" {
				s.seedAssetType(sym, qt, "Yahoo")
				yahooCount++
				continue
			}
		}
	}
	if ibCount > 0 || yahooCount > 0 {
		log.Printf("fundamentals: bootstrap complete: %d from IB, %d from Yahoo, %d already definitive", ibCount, yahooCount, skipCount)
	}
}

// isDefinitiveAssetType returns true when the type is settled and needs no external confirmation.
func isDefinitiveAssetType(t string) bool {
	switch t {
	case "ETF", "Bond ETF", "Stock", "Bond", "Commodity", "Mutual Fund":
		return true
	}
	return false
}

// ibAssetType maps the IB broker AssetCategory (from transactions table) to our AssetType.
// The parser resolves subCategory="ETF" on STK rows, so "ETF" arrives here directly.
// Plain "STK" means a common stock. Returns "" for unknown categories so Yahoo can arbitrate.
func (s *Service) ibAssetType(symbol string) string {
	var cat string
	s.DB.Model(&models.Transaction{}).
		Where("symbol = ? AND asset_category != ''", symbol).
		Limit(1).
		Pluck("asset_category", &cat)
	switch cat {
	case "ETF":
		return "ETF"
	case "STK":
		return "Stock"
	case "CMDTY", "WAR":
		return "Commodity"
	case "BOND":
		return "Bond"
	default:
		return ""
	}
}

// seedAssetType writes an AssetType into the DB for symbol.
// Creates a new record if none exists. If a record exists, only overwrites when
// its current AssetType is empty or "Unknown" — never downgrades a richer record.
// In particular, "Bond ETF" is never downgraded back to "ETF" even by IB.
func (s *Service) seedAssetType(symbol, assetType, source string) {
	now := time.Now().UTC()
	var rec models.AssetFundamental
	if err := s.DB.Where("symbol = ?", symbol).First(&rec).Error; err != nil {
		if err2 := s.DB.Create(&models.AssetFundamental{
			Symbol:      symbol,
			AssetType:   assetType,
			DataSource:  source,
			LastUpdated: now,
		}).Error; err2 != nil {
			log.Printf("fundamentals: seed create %s: %v", symbol, err2)
		}
		return
	}
	// Never downgrade Bond ETF → ETF (Bond ETF is the more specific classification).
	if rec.AssetType == "Bond ETF" && assetType == "ETF" {
		return
	}
	if rec.AssetType == "" || rec.AssetType == "Unknown" || source == "IB" {
		if err := s.DB.Model(&rec).Updates(map[string]interface{}{
			"asset_type":   assetType,
			"data_source":  source,
			"last_updated": now,
		}).Error; err != nil {
			log.Printf("fundamentals: seed update %s: %v", symbol, err)
		}
	}
}

// ── Tier 1: FMP queue ─────────────────────────────────────────────────────────

// buildFundamentalsQueue returns symbols for FMP enrichment, sorted oldest-first.
// A symbol is included when ALL of the following hold:
//   - It is not updated today (data doesn't change intra-day).
//   - Its AssetType is unresolved ("", "Unknown") OR its Country field is empty
//     (FMP has sector/country data we haven't fetched yet).
func (s *Service) buildFundamentalsQueue(symbols []string, today time.Time) []string {
	type entry struct {
		name        string
		lastUpdated time.Time
	}
	var queue []entry

	for _, sym := range symbols {
		var rec models.AssetFundamental
		if err := s.DB.Where("symbol = ?", sym).First(&rec).Error; err != nil {
			queue = append(queue, entry{sym, time.Time{}}) // no record → highest priority
			continue
		}
		if sameDay(rec.LastUpdated, today) {
			continue // fresh — skip
		}
		if rec.AssetType == "Unknown" || rec.AssetType == "" || rec.Country == "" {
			queue = append(queue, entry{sym, rec.LastUpdated})
		}
	}

	sort.Slice(queue, func(i, j int) bool {
		if queue[i].lastUpdated.IsZero() {
			return true
		}
		if queue[j].lastUpdated.IsZero() {
			return false
		}
		return queue[i].lastUpdated.Before(queue[j].lastUpdated)
	})

	out := make([]string, len(queue))
	for i, e := range queue {
		out[i] = e.name
	}
	return out
}

// fetchOneFundamentals tries each FundamentalsProvider in priority order for one symbol.
func (s *Service) fetchOneFundamentals(symbol string) {
	for _, p := range s.fundamentalsProviders {
		state := s.fundamentalsStates[p.Name()]
		cfg := p.RateLimit()
		if !state.available(cfg) {
			log.Printf("fundamentals: %s rate limited, skipping fundamentals for %s", p.Name(), symbol)
			continue
		}

		log.Printf("fundamentals: %s fetching fundamentals for %s", p.Name(), symbol)
		state.consume()
		fund, err := p.FetchFundamentals(symbol)
		if err != nil {
			log.Printf("fundamentals: %s error for %s: %v", p.Name(), symbol, err)
			if isRateLimitErr(err) {
				state.triggerCooldown(cfg)
			}
			continue
		}
		if fund == nil {
			log.Printf("fundamentals: %s profile not found for %s", p.Name(), symbol)
			// FMP has no profile — write a stub so we don't retry too aggressively.
			s.upsertFundamentals(symbol, &models.AssetFundamental{
				Symbol:      symbol,
				AssetType:   "Unknown",
				DataSource:  p.Name(),
				LastUpdated: time.Now().UTC(),
			})
			return
		}
		s.upsertFundamentals(symbol, fund)
		return
	}
}

// ── Tier 2: Yahoo quoteSummary breakdown queue ────────────────────────────────

// buildBreakdownQueue returns confirmed ETF/Bond ETF symbols for breakdown enrichment,
// where no breakdown rows were updated today.
func (s *Service) buildBreakdownQueue(symbols []string, today time.Time) []string {
	var queue []string
	for _, sym := range symbols {
		var rec models.AssetFundamental
		// Only confirmed equity or bond ETFs.
		if err := s.DB.Where("symbol = ? AND asset_type IN ?", sym, []string{"ETF", "Bond ETF"}).First(&rec).Error; err != nil {
			continue
		}
		// Skip if any breakdown row was updated today.
		var freshCount int64
		s.DB.Model(&models.EtfBreakdown{}).
			Where("fund_symbol = ? AND last_updated >= ?", sym, startOfDay(today)).
			Count(&freshCount)
		if freshCount > 0 {
			continue
		}
		queue = append(queue, sym)
	}
	return queue
}

// fetchOneBreakdown fetches aggregate sector/country/bond-rating breakdown for one ETF.
func (s *Service) fetchOneBreakdown(fundSymbol string) {
	for _, p := range s.breakdownProviders {
		state := s.breakdownStates[p.Name()]
		cfg := p.RateLimit()
		if !state.available(cfg) {
			log.Printf("fundamentals: %s rate limited, skipping breakdown for %s", p.Name(), fundSymbol)
			continue
		}

		log.Printf("fundamentals: %s fetching breakdown for %s", p.Name(), fundSymbol)
		state.consume()
		data, err := p.FetchETFBreakdown(fundSymbol)
		if err != nil {
			log.Printf("fundamentals: %s breakdown error for %s: %v", p.Name(), fundSymbol, err)
			if isRateLimitErr(err) {
				state.triggerCooldown(cfg)
			}
			continue
		}

		if data == nil || len(data.Rows) == 0 {
			log.Printf("fundamentals: %s no breakdown data for %s", p.Name(), fundSymbol)
			return
		}

		s.upsertBreakdowns(fundSymbol, data.Rows)

		// If Yahoo identified this as a bond ETF, update asset type and duration.
		if data.IsBondETF {
			log.Printf("fundamentals: %s is a bond ETF (duration=%.2fy), updating asset type", fundSymbol, func() float64 {
				if data.Duration != nil {
					return *data.Duration
				}
				return 0
			}())
			s.updateBondETFMeta(fundSymbol, data.Duration)
		}
		return
	}
}

// updateBondETFMeta sets asset_type="Bond ETF" and stores duration for a confirmed bond ETF.
func (s *Service) updateBondETFMeta(symbol string, duration *float64) {
	updates := map[string]interface{}{
		"asset_type":   "Bond ETF",
		"last_updated": time.Now().UTC(),
	}
	if duration != nil {
		updates["duration"] = *duration
	}
	if err := s.DB.Model(&models.AssetFundamental{}).Where("symbol = ?", symbol).Updates(updates).Error; err != nil {
		log.Printf("fundamentals: updateBondETFMeta %s: %v", symbol, err)
	}
}

// ── DB helpers ────────────────────────────────────────────────────────────────

// upsertFundamentals writes/updates an AssetFundamental row (full record from FMP).
func (s *Service) upsertFundamentals(symbol string, f *models.AssetFundamental) {
	err := s.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "symbol"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "country", "sector",
			"exchange", "data_source", "last_updated",
		}),
	}).Create(f).Error
	if err != nil {
		log.Printf("fundamentals: upsert %s: %v", symbol, err)
	}
}

// upsertBreakdowns replaces all EtfBreakdown rows for a fund (delete-then-insert).
func (s *Service) upsertBreakdowns(fundSymbol string, rows []models.EtfBreakdown) {
	if err := s.DB.Where("fund_symbol = ?", fundSymbol).Delete(&models.EtfBreakdown{}).Error; err != nil {
		log.Printf("fundamentals: delete breakdowns for %s: %v", fundSymbol, err)
		return
	}
	if err := s.DB.Create(&rows).Error; err != nil {
		log.Printf("fundamentals: insert breakdowns for %s: %v", fundSymbol, err)
	}
}

// GetFundamentals returns cached AssetFundamental for symbol (no external calls).
func (s *Service) GetFundamentals(symbol string) (*models.AssetFundamental, error) {
	var f models.AssetFundamental
	if err := s.DB.Where("symbol = ?", symbol).First(&f).Error; err != nil {
		return nil, nil
	}
	return &f, nil
}

// GetBreakdowns returns cached EtfBreakdown rows for a fund (no external calls).
func (s *Service) GetBreakdowns(fundSymbol string) ([]models.EtfBreakdown, error) {
	var rows []models.EtfBreakdown
	if err := s.DB.Where("fund_symbol = ?", fundSymbol).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("get breakdowns %s: %w", fundSymbol, err)
	}
	return rows, nil
}

// ── Time helpers ──────────────────────────────────────────────────────────────

// todayUTC returns the start of the current day in UTC.
func todayUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// startOfDay returns midnight UTC for the given day value.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// sameDay returns true when t and ref fall on the same UTC calendar day.
func sameDay(t, ref time.Time) bool {
	ty, tm, td := t.UTC().Date()
	ry, rm, rd := ref.UTC().Date()
	return ty == ry && tm == rm && td == rd
}

// isRateLimitErr returns true when the error indicates a provider rate-limit condition.
func isRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "rate limit") || strings.Contains(msg, "429")
}

// isDailyRateLimitErr returns true when the error indicates the provider's daily quota is exhausted.
func isDailyRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "daily rate limit") || strings.Contains(msg, "daily quota")
}
