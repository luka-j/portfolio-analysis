package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"portfolio-analysis/config"
	"portfolio-analysis/db"
	"portfolio-analysis/router"
	breakdownsvc "portfolio-analysis/services/breakdown"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/fundamentals"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/llm"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/portfolio"
	"portfolio-analysis/services/tax"
)

func main() {
	cfg := config.Load()

	// Connect to Database via GORM
	database, err := db.Init(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database init failed: %v", err)
	}

	// Build services.
	marketSvc := market.NewYahooFinanceService(database)
	cnbSvc := market.NewCNBProvider(database)
	fxSvc := fx.NewService(marketSvc, cnbSvc)
	repo := flexquery.NewRepository(database)
	portfolioSvc := portfolio.NewService(marketSvc, fxSvc, marketSvc)
	taxSvc := tax.NewService(fxSvc)

	// Build fundamentals / breakdown providers from config.
	yahooFundamentalsProvider := fundamentals.NewYahooFundamentalsProvider(marketSvc, 30)
	yahooBreakdownProvider := fundamentals.NewYahooBreakdownProvider(marketSvc, 30, 500)

	allFundamentals := map[string]fundamentals.FundamentalsProvider{
		"Yahoo": yahooFundamentalsProvider,
	}
	allBreakdowns := map[string]fundamentals.ETFBreakdownProvider{
		"Yahoo": yahooBreakdownProvider,
	}

	fundamentalsSvc := fundamentals.BuildFromConfig(
		database,
		cfg.FundamentalsProviders,
		cfg.BreakdownProviders,
		allFundamentals,
		allBreakdowns,
		marketSvc,
	)
	breakdownService := breakdownsvc.NewService(database)

	llmService := llm.NewService(cfg.GeminiAPIKey, cfg.GeminiFlashModel, cfg.GeminiProModel, database, portfolioSvc)

	// Build Gin engine.
	r := router.SetupRouter(cfg, repo, database, marketSvc, marketSvc, fxSvc, portfolioSvc, taxSvc, fundamentalsSvc, breakdownService, llmService)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	// Start background fundamentals fetcher. ctx is cancelled on shutdown.
	ctx, cancelFundamentals := context.WithCancel(context.Background())
	fundamentalsSvc.StartBackgroundFetcher(ctx)

	go func() {
		log.Printf("Starting portfolio-analysis on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	cancelFundamentals()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := srv.Shutdown(ctx2); err != nil {
		log.Fatal("Server forced to shutdown: ", err)
	}
	log.Println("Server exiting")
}
