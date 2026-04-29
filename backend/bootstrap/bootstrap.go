package bootstrap

import (
	"portfolio-analysis/config"
	breakdownsvc "portfolio-analysis/services/breakdown"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/fundamentals"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/llm"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/portfolio"
	"portfolio-analysis/services/tax"

	"gorm.io/gorm"
)

// AppServices holds all application-level services wired together.
// Pass this struct to SetupRouter rather than individual positional parameters.
type AppServices struct {
	// Market is the Yahoo Finance service; it satisfies both market.Provider and market.CurrencyGetter.
	Market       *market.YahooFinanceService
	FX           *fx.Service
	Repo         *flexquery.Repository
	Portfolio    *portfolio.Service
	Tax          *tax.Service
	Fundamentals *fundamentals.Service
	Breakdown    *breakdownsvc.Service
	LLM          *llm.Service
}

// Build wires together all application services from config and a live database handle.
func Build(cfg *config.Config, database *gorm.DB) *AppServices {
	marketSvc := market.NewYahooFinanceService(database)
	cnbSvc := market.NewCNBProvider(database)
	fxSvc := fx.NewService(marketSvc, cnbSvc)
	repo := flexquery.NewRepository(database)
	portfolioSvc := portfolio.NewService(marketSvc, fxSvc, cfg.CashBucketExpiryDays)
	taxSvc := tax.NewService(fxSvc)

	yahooFundamentalsProvider := fundamentals.NewYahooFundamentalsProvider(marketSvc, 30)
	yahooBreakdownProvider := fundamentals.NewYahooBreakdownProvider(marketSvc, 30, 500)

	fundamentalsSvc := fundamentals.BuildFromConfig(
		database,
		cfg.FundamentalsProviders,
		cfg.BreakdownProviders,
		map[string]fundamentals.FundamentalsProvider{"Yahoo": yahooFundamentalsProvider},
		map[string]fundamentals.ETFBreakdownProvider{"Yahoo": yahooBreakdownProvider},
		marketSvc,
	)

	return &AppServices{
		Market:       marketSvc,
		FX:           fxSvc,
		Repo:         repo,
		Portfolio:    portfolioSvc,
		Tax:          taxSvc,
		Fundamentals: fundamentalsSvc,
		Breakdown:    breakdownsvc.NewService(database),
		LLM:          llm.NewService(cfg.GeminiAPIKey, cfg.GeminiFlashModel, cfg.GeminiProModel, cfg.GeminiDefaultModel, database, portfolioSvc),
	}
}
