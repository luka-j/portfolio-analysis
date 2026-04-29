package main

import (
	"context"
	"embed"
	"fmt"
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

	"portfolio-analysis/bootstrap"
	"portfolio-analysis/config"
	"portfolio-analysis/db"
	"portfolio-analysis/router"
	"portfolio-analysis/services/fundamentals"
)

//go:embed all:frontend/dist
var embeddedFrontend embed.FS


func main() {
	cfg := config.Load()

	database, err := db.Init(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database init failed: %v", err)
	}

	svc := bootstrap.Build(cfg, database)
	r := router.SetupRouter(cfg, database, svc)

	fileSystem, frontendMode := buildFrontendFS()
	setupFrontendRoutes(r, fileSystem)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	logStartupSummary(cfg, frontendMode)
	runServer(srv, svc.Fundamentals)
}


// buildFrontendFS returns the HTTP filesystem for the frontend assets and a label describing
// the source. If FRONTEND_DIR is set, files are served from disk (useful during development);
// otherwise they are served from the embedded binary.
func buildFrontendFS() (http.FileSystem, string) {
	if dir := os.Getenv("FRONTEND_DIR"); dir != "" {
		return http.Dir(dir), "disk (" + dir + ")"
	}
	sub, err := fs.Sub(embeddedFrontend, "frontend/dist")
	if err != nil {
		log.Fatalf("Failed to access embedded frontend: %v", err)
	}
	return http.FS(sub), "embedded"
}

// setupFrontendRoutes registers the catch-all NoRoute handler that serves the React SPA.
// Static assets are served directly; all other paths fall back to index.html.
func setupFrontendRoutes(r *gin.Engine, fileSystem http.FileSystem) {
	fileServer := http.FileServer(fileSystem)
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
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
	})
}

// logStartupSummary prints a human-readable summary of the active configuration.
func logStartupSummary(cfg *config.Config, frontendMode string) {
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
	log.Printf("portfolio-analysis starting on :%s", cfg.Port)
	log.Printf("  Open:      http://localhost:%s", cfg.Port)
	log.Printf("  Database:  %s", dbLabel)
	log.Printf("  Auth:      %s", authMode)
	log.Printf("  LLM:       %s", llmStatus)
	log.Printf("  Frontend:  %s", frontendMode)
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
