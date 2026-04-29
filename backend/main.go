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

	"portfolio-analysis/bootstrap"
	"portfolio-analysis/config"
	"portfolio-analysis/db"
	"portfolio-analysis/router"
	"portfolio-analysis/services/fundamentals"
)

func main() {
	cfg := config.Load()

	database, err := db.Init(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database init failed: %v", err)
	}

	svc := bootstrap.Build(cfg, database)
	r := router.SetupRouter(cfg, database, svc)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	logStartupSummary(cfg)
	runServer(srv, svc.Fundamentals)
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
