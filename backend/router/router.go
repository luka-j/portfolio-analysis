package router

import (
	"fmt"
	"log"
	"net/http"
	"time"

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
	scenariosvc "portfolio-analysis/services/scenario"
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

	// Scenario repository — shared across handlers that support scenario_id.
	scenarioRepo := scenariosvc.NewRepository(database)

	// Evict unpinned scenarios older than 7 days; run once at startup then daily.
	go func() {
		evict := func() {
			n, labels, err := scenarioRepo.EvictStaleUnpinned(time.Now().UTC().AddDate(0, 0, -7))
			if err != nil {
				log.Printf("WARN: scenario eviction: %v", err)
			} else if n > 0 {
				log.Printf("Evicted %d stale unpinned scenarios: %v", n, labels)
			}
		}
		evict()
		for range time.Tick(24 * time.Hour) {
			evict()
		}
	}()

	// Shared ScenarioMiddleware wired to all handlers that support scenario_id.
	scenarioMiddleware := handlers.ScenarioMiddleware{
		ScenarioRepo: scenarioRepo,
		ScenarioMkt:  marketSvc,
		ScenarioFX:   fxSvc,
	}

	// API v1 group with auth.
	api := r.Group("/api/v1")
	api.Use(middleware.TokenAuth(cfg.AllowedTokenHashes))

	// Auth check — returns 200 if the token is valid, 401 otherwise (via middleware).
	api.GET("/auth/verify", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Portfolio endpoints.
	ph := handlers.NewPortfolioHandler(repo, portfolioSvc, fxSvc)
	ph.ScenarioMiddleware = scenarioMiddleware
	api.POST("/portfolio/upload", ph.Upload)
	api.POST("/portfolio/upload/etrade/benefits", ph.UploadEtradeBenefits)
	api.POST("/portfolio/upload/etrade/sales", ph.UploadEtradeSales)
	api.GET("/portfolio/value", ph.GetValue)
	api.GET("/portfolio/history", ph.GetHistory)
	api.GET("/portfolio/history/returns", ph.GetReturns)
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
	api.GET("/market/symbols", mh.GetSymbols)

	// Stats endpoints.
	sh := handlers.NewStatsHandler(repo, portfolioSvc, marketSvc, fxSvc, currencyGetter)
	sh.ScenarioMiddleware = scenarioMiddleware
	api.GET("/portfolio/stats", sh.GetStats)
	api.GET("/portfolio/compare", sh.Compare)
	api.GET("/portfolio/standalone", sh.GetStandalone)
	api.GET("/portfolio/drawdown", sh.GetDrawdown)
	api.GET("/portfolio/rolling", sh.GetRolling)
	api.GET("/portfolio/attribution", sh.GetAttribution)
	api.GET("/portfolio/correlations", sh.GetCorrelations)
	api.GET("/portfolio/cumulative", sh.GetCumulative)

	// Tax endpoints.
	th := &handlers.TaxHandler{
		ScenarioMiddleware: scenarioMiddleware,
		Repo:               repo,
		TaxSvc:             taxSvc,
	}
	api.POST("/tax/report", th.GetReport)

	// Breakdown endpoint.
	bh := handlers.NewBreakdownHandler(repo, portfolioSvc, breakdownService, fundamentalsSvc)
	bh.ScenarioMiddleware = scenarioMiddleware
	api.GET("/portfolio/breakdown", bh.GetBreakdown)

	// LLM endpoints.
	lh := handlers.NewLLMHandler(repo, database, llmService, portfolioSvc, taxSvc, marketSvc, currencyGetter, breakdownService, cfg.DefaultRiskFreeRate)
	lh.ScenarioMiddleware = scenarioMiddleware
	api.GET("/llm/available", lh.IsAvailable)
	api.GET("/llm/summary", lh.GetSummary)
	api.POST("/llm/chat", lh.Chat)

	// Scenario endpoints.
	sch := handlers.NewScenarioHandler(repo, database, scenarioRepo, portfolioSvc, taxSvc, marketSvc, currencyGetter, fxSvc, llmService)
	api.GET("/scenarios", sch.List)
	api.POST("/scenarios", sch.Create)
	api.GET("/scenarios/:id", sch.Get)
	api.PATCH("/scenarios/:id", sch.Update)
	api.DELETE("/scenarios/:id", sch.Delete)
	api.POST("/scenarios/compare-llm", sch.CompareLLM)

	return r
}
