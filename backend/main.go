package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gorm.io/gorm"

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

type services struct {
	market       *market.YahooFinanceService
	fx           *fx.Service
	repo         *flexquery.Repository
	portfolio    *portfolio.Service
	tax          *tax.Service
	fundamentals *fundamentals.Service
	breakdown    *breakdownsvc.Service
	llm          *llm.Service
}

func main() {
	cfg := config.Load()

	database, err := db.Init(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database init failed: %v", err)
	}

	svc := buildServices(cfg, database)
	r := router.SetupRouter(cfg, svc.repo, database, svc.market, svc.market, svc.fx, svc.portfolio, svc.tax, svc.fundamentals, svc.breakdown, svc.llm)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	logStartupSummary(cfg)
	runServer(srv, svc.fundamentals)
}

// buildServices wires together all application services.
func buildServices(cfg *config.Config, database *gorm.DB) *services {
	marketSvc := market.NewYahooFinanceService(database)
	cnbSvc := market.NewCNBProvider(database)
	fxSvc := fx.NewService(marketSvc, cnbSvc)
	repo := flexquery.NewRepository(database)
	portfolioSvc := portfolio.NewService(marketSvc, fxSvc, marketSvc, cfg.CashBucketExpiryDays)
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

	return &services{
		market:       marketSvc,
		fx:           fxSvc,
		repo:         repo,
		portfolio:    portfolioSvc,
		tax:          taxSvc,
		fundamentals: fundamentalsSvc,
		breakdown:    breakdownsvc.NewService(database),
		llm:          llm.NewService(cfg.GeminiAPIKey, cfg.GeminiFlashModel, cfg.GeminiProModel, cfg.GeminiDefaultModel, database, portfolioSvc),
	}
}

// logStartupSummary prints a human-readable summary of the active configuration.
func logStartupSummary(cfg *config.Config) {
	dbLabel := "SQLite (" + strings.TrimPrefix(cfg.DatabaseURL, "sqlite:") + ")"
	if strings.HasPrefix(cfg.DatabaseURL, "postgres://") || strings.HasPrefix(cfg.DatabaseURL, "postgresql://") ||
		(strings.Contains(cfg.DatabaseURL, "host=") && (strings.Contains(cfg.DatabaseURL, "user=") || strings.Contains(cfg.DatabaseURL, "dbname="))) {
		dbLabel = "PostgreSQL"
	}
	authMode := "open (no token required)"
	if len(cfg.AllowedTokenHashes) > 0 {
		authMode = fmt.Sprintf("protected (%d token(s) configured)", len(cfg.AllowedTokenHashes))
	}
	llmStatus := "disabled — set GEMINI_API_KEY to enable"
	if cfg.GeminiAPIKey != "" {
		llmStatus = fmt.Sprintf("enabled (flash=%s, pro=%s)", cfg.GeminiFlashModel, cfg.GeminiProModel)
	}
	log.Printf("portfolio-analysis API starting on :%s", cfg.Port)
	log.Printf("  API base:  http://localhost:%s/api/v1", cfg.Port)
	log.Printf("  Database:  %s", dbLabel)
	log.Printf("  Auth:      %s", authMode)
	log.Printf("  LLM:       %s", llmStatus)
	log.Printf("  Metrics:   http://localhost:%s/metrics", cfg.MetricsPort)
}

// runServer starts the background fundamentals fetcher, serves HTTP, and blocks until
// SIGINT or SIGTERM is received, then shuts down gracefully.
func runServer(srv *http.Server, fundamentalsSvc *fundamentals.Service) {
	ctx, cancelFundamentals := context.WithCancel(context.Background())
	fundamentalsSvc.StartBackgroundFetcher(ctx)

	go func() {
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
