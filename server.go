package main

import (
	"context"
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

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

//go:embed all:frontend/dist
var embeddedFrontend embed.FS

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

	llmService := llm.NewService(cfg.GeminiAPIKey, cfg.GeminiSummaryModel, cfg.GeminiChatModel, database, portfolioSvc)

	// Build Gin engine using the backend router setup.
	r := router.SetupRouter(cfg, repo, database, marketSvc, marketSvc, fxSvc, portfolioSvc, taxSvc, fundamentalsSvc, breakdownService, llmService)

	// Set up frontend file serving.
	// If FRONTEND_DIR is set, serve from disk (useful during development).
	// Otherwise, serve from the binary-embedded frontend/dist.
	var fileSystem http.FileSystem
	if dir := os.Getenv("FRONTEND_DIR"); dir != "" {
		fileSystem = http.Dir(dir)
		log.Printf("Serving frontend from disk: %s", dir)
	} else {
		sub, err := fs.Sub(embeddedFrontend, "frontend/dist")
		if err != nil {
			log.Fatalf("Failed to access embedded frontend: %v", err)
		}
		fileSystem = http.FS(sub)
		log.Printf("Serving frontend from embedded binary")
	}

	fileServer := http.FileServer(fileSystem)

	// NoRoute handles all non-API paths: static files + SPA fallback.
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		if strings.HasPrefix(path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "API route not found"})
			return
		}

		// Serve the file if it exists in the frontend FS.
		f, err := fileSystem.Open(path)
		if err == nil {
			info, err := f.Stat()
			f.Close()
			if err == nil && !info.IsDir() {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}

		// SPA fallback: serve index.html for all unknown paths.
		index, err := fileSystem.Open("/index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		defer index.Close()
		content, _ := io.ReadAll(index)
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	// Start background fundamentals fetcher. ctx is cancelled on shutdown.
	ctx, cancelFundamentals := context.WithCancel(context.Background())
	fundamentalsSvc.StartBackgroundFetcher(ctx)

	go func() {
		log.Printf("Starting unified portfolio-analysis on :%s", cfg.Port)
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
