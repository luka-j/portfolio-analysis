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

	"portfolio-analysis/models"
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
	var dbLabel string
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") ||
		(strings.Contains(dsn, "host=") && (strings.Contains(dsn, "user=") || strings.Contains(dsn, "dbname="))) {
		dialector = postgres.Open(dsn)
		dbLabel = "PostgreSQL"
	} else {
		// Assume SQLite. Strip optional "sqlite:" prefix for clarity.
		sqlitePath := strings.TrimPrefix(dsn, "sqlite:")
		dialector = sqlite.Open(sqlitePath)
		dbLabel = "SQLite (" + sqlitePath + ")"
	}

	database, err := gorm.Open(dialector, &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	log.Printf("Database: %s — running migrations", dbLabel)

	// Drop stale llm_caches unique index if it covers only (user_hash, prompt_type) —
	// the current schema requires all three columns (user_hash, prompt_type, model).
	// AutoMigrate never drops/recreates indexes, so we do it manually once.
	database.Exec(`DROP INDEX IF EXISTS idx_llmcache_user_prompt`)

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

	return database, nil
}
