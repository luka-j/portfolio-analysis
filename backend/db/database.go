package db

import (
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"gofolio-analysis/models"
)

// Init connects to the database via GORM and auto-migrates all tables.
func Init(dsn string) *gorm.DB {
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

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	log.Println("Migrating database schemas...")
	err = db.AutoMigrate(
		&models.User{},
		&models.Transaction{},
		&models.MarketData{},
	)
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// Cleanup any corrupted zero-price items previously saved due to Yahoo JSON null values
	result := db.Where("close = 0 AND volume != -1").Delete(&models.MarketData{})
	if result.RowsAffected > 0 {
		log.Printf("Cleaned up %d corrupted zero-price market data rows", result.RowsAffected)
	}

	return db
}
