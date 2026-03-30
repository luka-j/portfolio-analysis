package db

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitSQLite(t *testing.T) {
	dbPath := "test_gofolio.db"
	defer os.Remove(dbPath)

	// Test with sqlite: prefix
	database, err := Init("sqlite:" + dbPath)
	assert.NoError(t, err)
	assert.NotNil(t, database)
	
	// Test if it can actually write/read something (migration should have run)
	// We can check if tables exist by querying GORM's Migrator
	assert.True(t, database.Migrator().HasTable("users"))
	assert.True(t, database.Migrator().HasTable("transactions"))
}

func TestInitSQLiteNoPrefix(t *testing.T) {
	dbPath := "test_gofolio_no_prefix.db"
	defer os.Remove(dbPath)

	// Test without sqlite: prefix
	database, err := Init(dbPath)
	assert.NoError(t, err)
	assert.NotNil(t, database)
	assert.True(t, database.Migrator().HasTable("users"))
}
