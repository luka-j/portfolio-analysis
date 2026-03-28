package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/config"
	"gofolio-analysis/db"
	"gofolio-analysis/handlers"
	"gofolio-analysis/middleware"
	breakdownsvc "gofolio-analysis/services/breakdown"
	"gofolio-analysis/services/flexquery"
	"gofolio-analysis/services/fundamentals"
	"gofolio-analysis/services/fx"
	"gofolio-analysis/services/market"
	"gofolio-analysis/services/portfolio"
	"gofolio-analysis/services/tax"
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
	parser := flexquery.NewParser(database)
	portfolioSvc := portfolio.NewService(marketSvc, fxSvc)
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

	// Build Gin engine.
	r := setupRouter(cfg, parser, marketSvc, marketSvc, fxSvc, portfolioSvc, taxSvc, fundamentalsSvc, breakdownService)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	// Start background fundamentals fetcher. ctx is cancelled on shutdown.
	ctx, cancelFundamentals := context.WithCancel(context.Background())
	fundamentalsSvc.StartBackgroundFetcher(ctx)

	go func() {
		log.Printf("Starting gofolio-analysis on :%s", cfg.Port)
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

// setupRouter creates the Gin engine with all routes wired. Exported for testing.
func setupRouter(
	cfg *config.Config,
	parser *flexquery.Parser,
	marketSvc market.Provider,
	currencyGetter market.CurrencyGetter,
	fxSvc *fx.Service,
	portfolioSvc *portfolio.Service,
	taxSvc *tax.Service,
	fundamentalsSvc *fundamentals.Service,
	breakdownService *breakdownsvc.Service,
) *gin.Engine {
	r := gin.Default()

	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", cfg.CORSOrigin)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, X-Auth-Token")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// API v1 group with auth.
	api := r.Group("/api/v1")
	api.Use(middleware.TokenAuth(cfg.AllowedTokenHashes))

	// Health endpoint
	api.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Portfolio endpoints.
	ph := handlers.NewPortfolioHandler(parser, portfolioSvc)
	api.POST("/portfolio/upload", ph.Upload)
	api.POST("/portfolio/upload/etrade/benefits", ph.UploadEtradeBenefits)
	api.POST("/portfolio/upload/etrade/sales", ph.UploadEtradeSales)
	api.GET("/portfolio/value", ph.GetValue)
	api.GET("/portfolio/history", ph.GetHistory)
	api.GET("/portfolio/history/returns", ph.GetReturns) // real cumulative TWR curve
	api.GET("/portfolio/trades", ph.GetTrades)
	api.PUT("/portfolio/symbols/:symbol/mapping", ph.MapSymbol)

	// Market endpoints.
	mh := handlers.NewMarketHandler(marketSvc, currencyGetter)
	api.GET("/market/history", mh.GetHistory)

	// Stats endpoints.
	sh := handlers.NewStatsHandler(parser, portfolioSvc, marketSvc, fxSvc, currencyGetter)
	api.GET("/portfolio/stats", sh.GetStats)
	api.GET("/portfolio/compare", sh.Compare)

	// Tax endpoints.
	th := &handlers.TaxHandler{Parser: parser, TaxSvc: taxSvc}
	api.GET("/tax/report", th.GetReport)

	// Breakdown endpoint.
	bh := handlers.NewBreakdownHandler(parser, portfolioSvc, breakdownService, fundamentalsSvc)
	api.GET("/portfolio/breakdown", bh.GetBreakdown)

	return r
}
