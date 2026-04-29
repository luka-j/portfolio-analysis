package router

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"portfolio-analysis/bootstrap"
	"portfolio-analysis/config"
	"portfolio-analysis/services/market"
)

// SetupRouterWithMarket creates the Gin engine with an injectable market provider,
// intended for use in integration tests where a mock replaces the real Yahoo Finance service.
// The mock must implement market.Provider; if it also implements market.CurrencyGetter
// that capability is used for FX-conversion in benchmark/standalone metrics.
func SetupRouterWithMarket(cfg *config.Config, database *gorm.DB, svc *bootstrap.AppServices, mp market.Provider) *gin.Engine {
	cg, _ := mp.(market.CurrencyGetter)
	return buildRouter(cfg, database, svc, mp, cg)
}
