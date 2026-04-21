package router

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	ginprometheus "github.com/zsais/go-gin-prometheus"
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

	// Prometheus metrics — instrumented per-path; served on a separate port.
	p := ginprometheus.NewPrometheus("gin")
	p.SetListenAddress(":" + cfg.MetricsPort)
	p.Use(r)

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

	// Health endpoint — unauthenticated, used by load balancers and container runtimes.
	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// API v1 group with auth.
	api := r.Group("/api/v1")
	api.Use(middleware.TokenAuth(cfg.AllowedTokenHashes))

	// Auth check — returns 200 if the token is valid, 401 otherwise (via middleware).
	api.GET("/auth/verify", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
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
	api.PUT("/portfolio/assets/:symbol", ph.EditAsset)
	api.POST("/portfolio/transactions", ph.AddTransaction)
	api.DELETE("/portfolio/transactions/:id", ph.DeleteTransaction)

	// Market endpoints.
	mh := handlers.NewMarketHandler(marketSvc, currencyGetter, database)
	api.GET("/market/history", mh.GetHistory)
	api.GET("/market/security-chart", mh.GetSecurityChart)

	// Stats endpoints.
	sh := handlers.NewStatsHandler(repo, portfolioSvc, marketSvc, fxSvc, currencyGetter)
	api.GET("/portfolio/stats", sh.GetStats)
	api.GET("/portfolio/compare", sh.Compare)
	api.GET("/portfolio/standalone", sh.GetStandalone)
	api.GET("/portfolio/drawdown", sh.GetDrawdown)
	api.GET("/portfolio/rolling", sh.GetRolling)
	api.GET("/portfolio/attribution", sh.GetAttribution)
	api.GET("/portfolio/correlations", sh.GetCorrelations)
	api.GET("/portfolio/cumulative", sh.GetCumulative)

	// Tax endpoints.
	th := &handlers.TaxHandler{Repo: repo, TaxSvc: taxSvc}
	api.POST("/tax/report", th.GetReport)

	// Breakdown endpoint.
	bh := handlers.NewBreakdownHandler(repo, portfolioSvc, breakdownService, fundamentalsSvc)
	api.GET("/portfolio/breakdown", bh.GetBreakdown)

	// LLM endpoints.
	lh := handlers.NewLLMHandler(repo, database, llmService, portfolioSvc, taxSvc, marketSvc, currencyGetter)
	api.GET("/llm/available", lh.IsAvailable)
	api.GET("/llm/summary", lh.GetSummary)
	api.POST("/llm/chat", lh.Chat)

	return r
}
