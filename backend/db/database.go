package db

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"gofolio-analysis/models"
)

// Init connects to the database via GORM and auto-migrates all tables.
func Init(dsn string) (*gorm.DB, error) {
	// Setup quiet logger for production, standard for dev
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)

	var dialector gorm.Dialector
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") ||
		(strings.Contains(dsn, "host=") && (strings.Contains(dsn, "user=") || strings.Contains(dsn, "dbname="))) {
		dialector = postgres.Open(dsn)
	} else {
		// Assume SQLite. Strip optional "sqlite:" prefix for clarity.
		sqlitePath := strings.TrimPrefix(dsn, "sqlite:")
		dialector = sqlite.Open(sqlitePath)
	}

	database, err := gorm.Open(dialector, &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	// Backfill model column (added later; existing rows used the pro/chat model).
	database.Exec(`UPDATE llm_caches SET model = 'pro' WHERE model IS NULL OR model = ''`)
	// Deduplicate llm_caches before migration adds/recreates the unique index.
	database.Exec(`
		DELETE FROM llm_caches
		WHERE id NOT IN (
			SELECT MAX(id) FROM llm_caches GROUP BY user_hash, prompt_type, model
		)
	`)

	log.Println("Migrating database schemas...")
	err = database.AutoMigrate(
		&models.User{},
		&models.Transaction{},
		&models.MarketData{},
		&models.AssetFundamental{},
		&models.EtfBreakdown{},
		&models.LLMCache{},
		&models.CurrentPrice{},
	)
	if err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	// Cleanup any corrupted zero-price items previously saved due to Yahoo JSON null values
	result := database.Where("close = 0 AND volume != -1").Delete(&models.MarketData{})
	if result.RowsAffected > 0 {
		log.Printf("Cleaned up %d corrupted zero-price market data rows", result.RowsAffected)
	}

	return database, nil
}
