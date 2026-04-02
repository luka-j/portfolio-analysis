package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/config"
	"gofolio-analysis/db"
	"gofolio-analysis/router"
	breakdownsvc "gofolio-analysis/services/breakdown"
	"gofolio-analysis/services/flexquery"
	"gofolio-analysis/services/fundamentals"
	"gofolio-analysis/services/fx"
	"gofolio-analysis/services/llm"
	"gofolio-analysis/services/market"
	"gofolio-analysis/services/portfolio"
	"gofolio-analysis/services/tax"
)

func main() {
	cfg := config.Load()

	frontendDir := os.Getenv("FRONTEND_DIR")
	if frontendDir == "" {
		frontendDir = "./frontend/dist"
	}

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

	llmService := llm.NewService(cfg.GeminiAPIKey, cfg.GeminiSummaryModel, cfg.GeminiChatModel, database, portfolioSvc)

	// Build Gin engine using the backend router setup.
	r := router.SetupRouter(cfg, repo, database, marketSvc, marketSvc, fxSvc, portfolioSvc, taxSvc, fundamentalsSvc, breakdownService, llmService)

	// Serve static files from the frontend build directory.
	// We use NoRoute to implement SPA fallback (serving index.html for unknown routes).
	r.StaticFS("/assets", http.Dir(filepath.Join(frontendDir, "assets")))
	
	// Serve other static files in the root of FrontendDir (like favicon, etc.)
	// We do this manually to avoid shadowing the API routes.
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		
		// If it's an API route that reached here, it's a 404 API.
		if strings.HasPrefix(path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "API route not found"})
			return
		}

		// Try to serve static file from FrontendDir.
		fullPath := filepath.Join(frontendDir, filepath.FromSlash(path))
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			c.File(fullPath)
			return
		}

		// Otherwise, serve index.html for SPA routing.
		c.File(filepath.Join(frontendDir, "index.html"))
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	// Start background fundamentals fetcher. ctx is cancelled on shutdown.
	ctx, cancelFundamentals := context.WithCancel(context.Background())
	fundamentalsSvc.StartBackgroundFetcher(ctx)

	go func() {
		log.Printf("Starting unified gofolio-analysis on :%s", cfg.Port)
		log.Printf("Serving frontend from: %s", frontendDir)
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
