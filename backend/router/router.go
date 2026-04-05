package router

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"portfolio-analysis/config"
	"portfolio-analysis/handlers"
	"portfolio-analysis/middleware"
	breakdownsvc "portfolio-analysis/services/breakdown"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/fundamentals"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/llm"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/portfolio"
	"portfolio-analysis/services/tax"
)

// SetupRouter creates the Gin engine with all routes wired.
func SetupRouter(
	cfg *config.Config,
	repo *flexquery.Repository,
	database *gorm.DB,
	marketSvc market.Provider,
	currencyGetter market.CurrencyGetter,
	fxSvc *fx.Service,
	portfolioSvc *portfolio.Service,
	taxSvc *tax.Service,
	fundamentalsSvc *fundamentals.Service,
	breakdownService *breakdownsvc.Service,
	llmService *llm.Service,
) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.RecoveryWithWriter(gin.DefaultErrorWriter, func(c *gin.Context, err any) {
		log.Printf("panic recovered: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("internal server error: %v", err)})
	}))

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
	ph := handlers.NewPortfolioHandler(repo, portfolioSvc, fxSvc)
	api.POST("/portfolio/upload", ph.Upload)
	api.POST("/portfolio/upload/etrade/benefits", ph.UploadEtradeBenefits)
	api.POST("/portfolio/upload/etrade/sales", ph.UploadEtradeSales)
	api.GET("/portfolio/value", ph.GetValue)
	api.GET("/portfolio/history", ph.GetHistory)
	api.GET("/portfolio/history/returns", ph.GetReturns) // real cumulative TWR curve
	api.GET("/portfolio/trades", ph.GetTrades)
	api.GET("/portfolio/price-history", ph.GetPriceHistory)
	api.PUT("/portfolio/symbols/:symbol/mapping", ph.MapSymbol)

	// Market endpoints.
	mh := handlers.NewMarketHandler(marketSvc, currencyGetter)
	api.GET("/market/history", mh.GetHistory)

	// Stats endpoints.
	sh := handlers.NewStatsHandler(repo, portfolioSvc, marketSvc, fxSvc, currencyGetter)
	api.GET("/portfolio/stats", sh.GetStats)
	api.GET("/portfolio/compare", sh.Compare)
	api.GET("/portfolio/standalone", sh.GetStandalone)

	// Tax endpoints.
	th := &handlers.TaxHandler{Repo: repo, TaxSvc: taxSvc}
	api.POST("/tax/report", th.GetReport)

	// Breakdown endpoint.
	bh := handlers.NewBreakdownHandler(repo, portfolioSvc, breakdownService, fundamentalsSvc)
	api.GET("/portfolio/breakdown", bh.GetBreakdown)

	// LLM endpoints.
	lh := handlers.NewLLMHandler(repo, database, llmService)
	api.GET("/llm/available", lh.IsAvailable)
	api.GET("/llm/summary", lh.GetSummary)
	api.POST("/llm/chat", lh.Chat)

	return r
}
