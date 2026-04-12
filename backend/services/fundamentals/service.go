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

	"portfolio-analysis/models"
)

// QuoteTypeFetcher resolves a symbol's asset class and display name using a free price API (no premium quota).
// Implemented by *market.YahooFinanceService.
type QuoteTypeFetcher interface {
	GetQuoteType(symbol string) (string, string, error)
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
//  3. Fundamentals provider — name / country / sector for symbols still incomplete or stale.
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

	// Step 3: fundamentals provider enrichment (name / country / sector) for stale or incomplete records.
	funQueue := s.buildFundamentalsQueue(allSymbols, today)
	if len(funQueue) > 0 {
		log.Printf("fundamentals: queueing %d symbols for fundamentals enrichment", len(funQueue))
	} else {
		log.Println("fundamentals: no symbols need fundamentals enrichment (all fresh or definitive)")
	}

	for _, entry := range funQueue {
		if ctx.Err() != nil {
			return
		}
		s.fetchOneFundamentals(entry)
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

// symbolWithUsers pairs an effective ticker with all user IDs that hold it.
// The background job fetches provider data once per symbol and writes per-user rows.
type symbolWithUsers struct {
	Symbol  string // effective ticker (YahooSymbol if set, otherwise broker Symbol)
	UserIDs []uint // sorted list of user IDs holding this symbol
}

// collectAllSymbols returns portfolio-level symbols that have been priced by Yahoo.
// Comes from the database: the function is restart-safe.
func (s *Service) collectAllSymbols() ([]symbolWithUsers, error) {
	return s.collectPortfolioSymbols()
}

// collectPortfolioSymbols returns effective ticker symbols grouped by the users who hold them.
// A symbol is included only when it has a YahooSymbol override or has existing market data.
// Provider data is fetched once per symbol and written to each user's row independently.
func (s *Service) collectPortfolioSymbols() ([]symbolWithUsers, error) {
	type row struct {
		Symbol      string
		YahooSymbol string
		UserID      uint
	}
	var rows []row
	err := s.DB.Model(&models.Transaction{}).
		Select("DISTINCT user_id, symbol, yahoo_symbol").
		Where("type IN ?", []string{"Trade", "ESPP_VEST", "RSU_VEST"}).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("collect portfolio symbols: %w", err)
	}

	type symInfo struct {
		userIDs map[uint]struct{}
		include bool
		checked bool
	}
	symMap := make(map[string]*symInfo)

	for _, r := range rows {
		eff := effectiveSymbol(r.Symbol, r.YahooSymbol)
		info, ok := symMap[eff]
		if !ok {
			info = &symInfo{userIDs: make(map[uint]struct{})}
			symMap[eff] = info
		}
		info.userIDs[r.UserID] = struct{}{}

		if !info.checked {
			info.checked = true
			if r.YahooSymbol != "" {
				info.include = true // user explicitly mapped — always include
			} else {
				info.include = s.hasMarketData(eff)
				if !info.include {
					log.Printf("fundamentals: skipping %q — no YahooSymbol and no market data", eff)
				}
			}
		}
	}

	var out []symbolWithUsers
	for sym, info := range symMap {
		if !info.include {
			continue
		}
		var userIDs []uint
		for uid := range info.userIDs {
			userIDs = append(userIDs, uid)
		}
		sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })
		out = append(out, symbolWithUsers{Symbol: sym, UserIDs: userIDs})
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
// The fundamentals provider is NOT called here. Symbols that remain unknown after
// both free sources are left for the fundamentals provider in buildFundamentalsQueue.
func (s *Service) bootstrapAssetTypes(ctx context.Context, symbols []symbolWithUsers) {
	ibCount, yahooCount, skipCount := 0, 0, 0
	for _, entry := range symbols {
		sym := entry.Symbol
		if ctx.Err() != nil {
			return
		}

		// Always seed conid and ISIN from IB transactions — idempotent, and must run even
		// for already-classified symbols so that re-uploaded reports backfill these fields.
		// transactions.isin is the raw source; asset_fundamentals.isin is the display copy read by the API.
		// These UPDATE calls use WHERE symbol=? to touch all users' rows (universal property).
		conid := s.ibConid(sym)
		s.seedConid(sym, conid)
		s.seedISIN(conid, s.ibISINForConid(conid))

		// Skip if ALL users already have a definitive IB classification.
		if s.allUsersDefinitiveIB(sym, entry.UserIDs) {
			skipCount++
			continue
		}

		// Tier 0: IB broker category — definitive for ETF, Commodity, Bond; ambiguous for STK.
		if ibType := s.ibAssetType(sym); ibType != "" {
			s.seedAssetType(sym, ibType, "IB", "", entry.UserIDs)
			// IB transaction currency is authoritative — seed it without a Yahoo call.
			s.seedCurrency(sym, s.ibCurrency(sym))
			ibCount++
			continue
		}

		// Tier 0.5: Yahoo Finance quoteType — free, already made for pricing anyway.
		if s.QuoteTypeFetcher != nil {
			qt, name, err := s.QuoteTypeFetcher.GetQuoteType(sym)
			if err != nil {
				log.Printf("fundamentals: Yahoo quoteType %s error: %v", sym, err)
			} else if qt != "" {
				s.seedAssetType(sym, qt, "Yahoo", name, entry.UserIDs)
				// Currency for non-IB symbols will be populated on first GetCurrency call.
				yahooCount++
				continue
			}
		}
	}
	if ibCount > 0 || yahooCount > 0 {
		log.Printf("fundamentals: bootstrap complete: %d from IB, %d from Yahoo, %d already definitive", ibCount, yahooCount, skipCount)
	}
}

// allUsersDefinitiveIB returns true when every user in userIDs already has a definitively
// IB-classified row for symbol. Used as a fast-path skip in bootstrapAssetTypes.
func (s *Service) allUsersDefinitiveIB(symbol string, userIDs []uint) bool {
	if len(userIDs) == 0 {
		return false
	}
	var count int64
	s.DB.Model(&models.AssetFundamental{}).
		Where("symbol = ? AND user_id IN ? AND data_source = 'IB' AND asset_type IN ?",
			symbol, userIDs, []string{"ETF", "Bond ETF", "Stock", "Bond", "Commodity", "Mutual Fund"}).
		Count(&count)
	return int(count) >= len(userIDs)
}

// isDefinitiveAssetType returns true when the type is settled and needs no external confirmation.
func isDefinitiveAssetType(t string) bool {
	switch t {
	case "ETF", "Bond ETF", "Stock", "Bond", "Commodity", "Mutual Fund":
		return true
	}
	return false
}

// ibCurrency returns the trading currency for symbol from the transactions table.
// IB-provided currency is authoritative for portfolio symbols, so no Yahoo call is needed.
func (s *Service) ibCurrency(symbol string) string {
	var cur string
	s.DB.Model(&models.Transaction{}).
		Where("(symbol = ? OR yahoo_symbol = ?) AND currency != ''", symbol, symbol).
		Limit(1).
		Pluck("currency", &cur)
	return cur
}

// ibConid returns the IB contract ID for symbol from the transactions table.
func (s *Service) ibConid(symbol string) string {
	var conid string
	s.DB.Model(&models.Transaction{}).
		Where("(symbol = ? OR yahoo_symbol = ?) AND conid != ''", symbol, symbol).
		Limit(1).
		Pluck("conid", &conid)
	return conid
}

// ibISINForConid returns the ISIN for the given conid from the transactions table.
func (s *Service) ibISINForConid(conid string) string {
	if conid == "" {
		return ""
	}
	var isin string
	s.DB.Model(&models.Transaction{}).
		Where("conid = ? AND isin != ''", conid).
		Limit(1).
		Pluck("isin", &isin)
	return isin
}

// seedConid writes conid into an existing asset_fundamentals row for symbol,
// only when the row exists and has no conid set yet.
func (s *Service) seedConid(symbol, conid string) {
	if conid == "" {
		return
	}
	if err := s.DB.Model(&models.AssetFundamental{}).
		Where("symbol = ? AND (conid IS NULL OR conid = '')", symbol).
		Update("conid", conid).Error; err != nil {
		log.Printf("fundamentals: seedConid %s: %v", symbol, err)
	}
}

// seedISIN writes ISIN into an existing asset_fundamentals row identified by conid.
// Using conid as the key guarantees the correct row even when the symbol has been renamed.
func (s *Service) seedISIN(conid, isin string) {
	if conid == "" || isin == "" {
		return
	}
	if err := s.DB.Model(&models.AssetFundamental{}).
		Where("conid = ? AND (isin IS NULL OR isin = '')", conid).
		Update("isin", isin).Error; err != nil {
		log.Printf("fundamentals: seedISIN (conid=%s): %v", conid, err)
	}
}

// seedCurrency writes currency into an existing asset_fundamentals row for symbol,
// only when the row exists and has no currency set yet.
func (s *Service) seedCurrency(symbol, currency string) {
	if currency == "" {
		return
	}
	result := s.DB.Model(&models.AssetFundamental{}).
		Where("symbol = ? AND (currency IS NULL OR currency = '')", symbol).
		Update("currency", currency)
	if result.Error != nil {
		log.Printf("fundamentals: seedCurrency %s: %v", symbol, result.Error)
	}
}

// ibAssetType maps the IB broker AssetCategory (from transactions table) to our AssetType.
// The parser resolves subCategory="ETF" on STK rows, so "ETF" arrives here directly.
// Plain "STK" means a common stock. Returns "" for unknown categories so Yahoo can arbitrate.
func (s *Service) ibAssetType(symbol string) string {
	var cat string
	s.DB.Model(&models.Transaction{}).
		Where("(symbol = ? OR yahoo_symbol = ?) AND asset_category != ''", symbol, symbol).
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

// seedAssetType writes an AssetType (and optionally a name) into the DB for each user in userIDs.
// Creates a new per-user record if none exists. If a record exists, only overwrites when its
// current AssetType is empty or "Unknown" — never downgrades a richer record.
// In particular, "Bond ETF" is never downgraded back to "ETF" even by IB.
// Records with DataSource="User" are never overwritten.
// name is written only when non-empty and the existing record has no name yet.
func (s *Service) seedAssetType(symbol, assetType, source, name string, userIDs []uint) {
	now := time.Now().UTC()
	for _, userID := range userIDs {
		var rec models.AssetFundamental
		if err := s.DB.Where("user_id = ? AND symbol = ?", userID, symbol).First(&rec).Error; err != nil {
			newRec := models.AssetFundamental{
				UserID:      userID,
				Symbol:      symbol,
				AssetType:   assetType,
				DataSource:  source,
				LastUpdated: now,
			}
			if name != "" {
				newRec.Name = name
			}
			if err2 := s.DB.Create(&newRec).Error; err2 != nil {
				log.Printf("fundamentals: seed create %s (user=%d): %v", symbol, userID, err2)
			}
			continue
		}
		// Never touch user-edited records.
		if rec.DataSource == "User" {
			continue
		}
		// Never downgrade Bond ETF → ETF (Bond ETF is the more specific classification).
		if rec.AssetType == "Bond ETF" && assetType == "ETF" {
			continue
		}
		if rec.AssetType == "" || rec.AssetType == "Unknown" || source == "IB" {
			updates := map[string]interface{}{
				"asset_type":   assetType,
				"data_source":  source,
				"last_updated": now,
			}
			if name != "" && rec.Name == "" {
				updates["name"] = name
			}
			if err := s.DB.Model(&rec).Updates(updates).Error; err != nil {
				log.Printf("fundamentals: seed update %s (user=%d): %v", symbol, userID, err)
			}
		}
	}
}

// ── Tier 1: fundamentals enrichment queue ─────────────────────────────────────

// buildFundamentalsQueue returns entries needing fundamentals enrichment, sorted oldest-first.
// For each symbol, only the user IDs whose records are stale/incomplete (and not user-edited)
// are included in the returned entry. Provider data is fetched once per symbol.
func (s *Service) buildFundamentalsQueue(symbols []symbolWithUsers, today time.Time) []symbolWithUsers {
	type queueEntry struct {
		sw      symbolWithUsers
		oldest  time.Time
	}
	var queue []queueEntry

	for _, sw := range symbols {
		var needEnrichment []uint
		var oldest time.Time

		for _, userID := range sw.UserIDs {
			var rec models.AssetFundamental
			if err := s.DB.Where("user_id = ? AND symbol = ?", userID, sw.Symbol).First(&rec).Error; err != nil {
				needEnrichment = append(needEnrichment, userID) // no record — highest priority
				continue
			}
			if rec.DataSource == "User" {
				continue // user-edited; never overwrite
			}
			if sameDay(rec.LastUpdated, today) {
				continue // fresh — skip
			}
			if rec.AssetType == "Unknown" || rec.AssetType == "" || rec.Name == "" || rec.Country == "" {
				needEnrichment = append(needEnrichment, userID)
				if oldest.IsZero() || (!rec.LastUpdated.IsZero() && rec.LastUpdated.Before(oldest)) {
					oldest = rec.LastUpdated
				}
			}
		}

		if len(needEnrichment) > 0 {
			queue = append(queue, queueEntry{
				sw:     symbolWithUsers{Symbol: sw.Symbol, UserIDs: needEnrichment},
				oldest: oldest,
			})
		}
	}

	sort.Slice(queue, func(i, j int) bool {
		if queue[i].oldest.IsZero() {
			return true
		}
		if queue[j].oldest.IsZero() {
			return false
		}
		return queue[i].oldest.Before(queue[j].oldest)
	})

	out := make([]symbolWithUsers, len(queue))
	for i, e := range queue {
		out[i] = e.sw
	}
	return out
}

// fetchOneFundamentals fetches provider data once for entry.Symbol and upserts it for each
// user in entry.UserIDs whose record is not user-edited (DataSource != "User").
func (s *Service) fetchOneFundamentals(entry symbolWithUsers) {
	for _, p := range s.fundamentalsProviders {
		state := s.fundamentalsStates[p.Name()]
		cfg := p.RateLimit()
		if !state.available(cfg) {
			log.Printf("fundamentals: %s rate limited, skipping fundamentals for %s", p.Name(), entry.Symbol)
			continue
		}

		log.Printf("fundamentals: %s fetching fundamentals for %s", p.Name(), entry.Symbol)
		state.consume()
		fund, err := p.FetchFundamentals(entry.Symbol)
		if err != nil {
			log.Printf("fundamentals: %s error for %s: %v", p.Name(), entry.Symbol, err)
			if isRateLimitErr(err) {
				state.triggerCooldown(cfg)
			}
			continue
		}
		if fund == nil {
			log.Printf("fundamentals: %s profile not found for %s", p.Name(), entry.Symbol)
			// Provider has no profile — write a stub so we don't retry too aggressively.
			stub := &models.AssetFundamental{
				Symbol:      entry.Symbol,
				AssetType:   "Unknown",
				DataSource:  p.Name(),
				LastUpdated: time.Now().UTC(),
			}
			for _, uid := range entry.UserIDs {
				s.upsertFundamentals(entry.Symbol, stub, uid)
			}
			return
		}
		for _, uid := range entry.UserIDs {
			s.upsertFundamentals(entry.Symbol, fund, uid)
		}
		return
	}
}

// ── Tier 2: Yahoo quoteSummary breakdown queue ────────────────────────────────

// buildBreakdownQueue returns confirmed ETF/Bond ETF symbols for breakdown enrichment,
// where no breakdown rows were updated today.
func (s *Service) buildBreakdownQueue(symbols []symbolWithUsers, today time.Time) []string {
	var queue []string
	for _, sw := range symbols {
		if len(sw.UserIDs) == 0 {
			continue
		}
		// Asset type is universal — check the first available user's record.
		var rec models.AssetFundamental
		if err := s.DB.Where("user_id = ? AND symbol = ? AND asset_type IN ?",
			sw.UserIDs[0], sw.Symbol, []string{"ETF", "Bond ETF"}).First(&rec).Error; err != nil {
			continue
		}
		// Skip if any breakdown row was updated today.
		var freshCount int64
		s.DB.Model(&models.EtfBreakdown{}).
			Where("fund_symbol = ? AND last_updated >= ?", sw.Symbol, startOfDay(today)).
			Count(&freshCount)
		if freshCount > 0 {
			continue
		}
		queue = append(queue, sw.Symbol)
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
			var existing models.AssetFundamental
			alreadyBondETF := s.DB.Where("symbol = ? AND asset_type = 'Bond ETF'", fundSymbol).First(&existing).Error == nil
			if !alreadyBondETF {
				log.Printf("fundamentals: %s is a bond ETF (duration=%.2fy), promoting asset type", fundSymbol, func() float64 {
					if data.Duration != nil {
						return *data.Duration
					}
					return 0
				}())
			}
			s.updateBondETFMeta(fundSymbol, data.Duration)
		}
		return
	}
}

// updateBondETFMeta sets asset_type="Bond ETF" and stores duration for a confirmed bond ETF.
func (s *Service) updateBondETFMeta(symbol string, duration *float64) {
	// asset_type: skip user-edited rows so manual classifications are preserved.
	if err := s.DB.Model(&models.AssetFundamental{}).
		Where("symbol = ? AND data_source != 'User'", symbol).
		Update("asset_type", "Bond ETF").Error; err != nil {
		log.Printf("fundamentals: updateBondETFMeta asset_type %s: %v", symbol, err)
	}
	// duration + last_updated: always refresh — duration changes over time and users cannot set it.
	durationUpdates := map[string]interface{}{"last_updated": time.Now().UTC()}
	if duration != nil {
		durationUpdates["duration"] = *duration
	}
	if err := s.DB.Model(&models.AssetFundamental{}).
		Where("symbol = ?", symbol).
		Updates(durationUpdates).Error; err != nil {
		log.Printf("fundamentals: updateBondETFMeta duration %s: %v", symbol, err)
	}
}

// ── DB helpers ────────────────────────────────────────────────────────────────

// upsertFundamentals writes/updates an AssetFundamental row for the given user.
// Skips silently when the existing row has DataSource="User" (user-edited records are preserved).
func (s *Service) upsertFundamentals(symbol string, f *models.AssetFundamental, userID uint) {
	// Never overwrite records the user has manually edited.
	var existing models.AssetFundamental
	if s.DB.Where("user_id = ? AND symbol = ?", userID, symbol).First(&existing).Error == nil {
		if existing.DataSource == "User" {
			return
		}
	}
	row := *f // shallow copy so we can set UserID without mutating the shared template
	row.UserID = userID
	err := s.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "symbol"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "country", "sector",
			"data_source", "last_updated",
		}),
	}).Create(&row).Error
	if err != nil {
		log.Printf("fundamentals: upsert %s (user=%d): %v", symbol, userID, err)
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

// GetFundamentals returns cached AssetFundamental for symbol scoped to userID (no external calls).
func (s *Service) GetFundamentals(symbol string, userID uint) (*models.AssetFundamental, error) {
	var f models.AssetFundamental
	if err := s.DB.Where("user_id = ? AND symbol = ?", userID, symbol).First(&f).Error; err != nil {
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
