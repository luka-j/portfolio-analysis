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
	"gofolio-analysis/services/flexquery"
	"gofolio-analysis/services/fx"
	"gofolio-analysis/services/market"
	"gofolio-analysis/services/portfolio"
	"gofolio-analysis/services/tax"
)

func main() {
	cfg := config.Load()

	// Connect to Database via GORM
	database := db.Init(cfg.DatabaseURL)

	// Build services.
	marketSvc := market.NewYahooFinanceService(database)
	cnbSvc := market.NewCNBProvider(database)
	fxSvc := fx.NewService(marketSvc, cnbSvc)
	parser := flexquery.NewParser(database)
	portfolioSvc := portfolio.NewService(marketSvc, fxSvc)
	taxSvc := tax.NewService(fxSvc)

	// Build Gin engine.
	r := setupRouter(cfg, parser, marketSvc, fxSvc, portfolioSvc, taxSvc)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown: ", err)
	}
	log.Println("Server exiting")
}

// setupRouter creates the Gin engine with all routes wired. Exported for testing.
func setupRouter(cfg *config.Config, parser *flexquery.Parser, marketSvc market.Provider, fxSvc *fx.Service, portfolioSvc *portfolio.Service, taxSvc *tax.Service) *gin.Engine {
	r := gin.Default()

	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
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
	mh := handlers.NewMarketHandler(marketSvc)
	api.GET("/market/history", mh.GetHistory)

	// Stats endpoints.
	sh := handlers.NewStatsHandler(parser, portfolioSvc, marketSvc)
	api.GET("/portfolio/stats", sh.GetStats)
	api.GET("/portfolio/compare", sh.Compare)

	// Tax endpoints.
	th := &handlers.TaxHandler{Parser: parser, TaxSvc: taxSvc}
	api.GET("/tax/report", th.GetReport)

	return r
}
