package main

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
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
		slog.Error("database init failed", "err", err)
		os.Exit(1)
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
		slog.Error("failed to access embedded frontend", "err", err)
		os.Exit(1)
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
	slog.Info("portfolio-analysis starting",
		"addr", ":"+cfg.Port,
		"url", "http://localhost:"+cfg.Port,
		"database", dbLabel,
		"auth", authMode,
		"llm", llmStatus,
		"frontend", frontendMode,
		"metrics", "http://localhost:"+cfg.MetricsPort+"/metrics",
	)
}

// runServer starts the background fundamentals fetcher, serves HTTP, and blocks until
// SIGINT or SIGTERM is received, then shuts down gracefully.
func runServer(srv *http.Server, fundamentalsSvc *fundamentals.Service) {
	ctx, cancelFundamentals := context.WithCancel(context.Background())
	fundamentalsSvc.StartBackgroundFetcher(ctx)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server")

	cancelFundamentals()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := srv.Shutdown(ctx2); err != nil {
		slog.Error("server forced to shutdown", "err", err)
		os.Exit(1)
	}
	slog.Info("server exited")
}
