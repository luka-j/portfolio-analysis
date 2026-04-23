package db

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
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

	// Drop the old single-column unique index on asset_fundamentals.symbol.
	// The schema now uses a composite (user_id, symbol) unique index so each user
	// can have their own row per symbol. The old index blocks inserts for shared symbols.
	database.Exec(`DROP INDEX IF EXISTS idx_asset_fundamentals_symbol`)

	err = database.AutoMigrate(
		&models.User{},
		&models.Transaction{},
		&models.MarketData{},
		&models.AssetFundamental{},
		&models.EtfBreakdown{},
		&models.LLMCache{},
		&models.CurrentPrice{},
		&models.CorporateActionRecord{},
		&models.CashDividendRecord{},
		&models.ScenarioRecord{},
	)
	if err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	// Backfill PublicID for transaction rows created before UUID support was added.
	// The column was added as nullable so AutoMigrate succeeds on existing tables.
	// After backfilling, we set NOT NULL to enforce the constraint going forward.
	var missingIDs []models.Transaction
	if err := database.Where("public_id IS NULL OR public_id = ''").Find(&missingIDs).Error; err == nil {
		for i := range missingIDs {
			database.Model(&missingIDs[i]).Update("public_id", uuid.New().String())
		}
		if len(missingIDs) > 0 {
			log.Printf("Backfilled PublicID for %d existing transaction rows", len(missingIDs))
		}
	}

	// Apply NOT NULL constraint after the backfill so no rows are left with NULL.
	// SQLite does not support ALTER COLUMN, so we skip it there (the BeforeCreate
	// hook enforces non-empty values for all new rows regardless).
	if dbLabel == "PostgreSQL" {
		database.Exec(`ALTER TABLE transactions ALTER COLUMN public_id SET NOT NULL`)
	}

	return database, nil
}
