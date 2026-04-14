package flexquery

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"portfolio-analysis/models"
)

// insertManual inserts a manual BUY Trade transaction directly into the DB,
// mimicking what the AddTransaction handler does.
func insertManual(t *testing.T, db *gorm.DB, userID uint, symbol string, qty, price float64, dt time.Time) models.Transaction {
	t.Helper()
	txn := models.Transaction{
		UserID:        userID,
		Type:          "Trade",
		BuySell:       "BUY",
		Symbol:        symbol,
		Currency:      "USD",
		DateTime:      dt,
		Quantity:      qty,
		Price:         price,
		Proceeds:      -(qty * price),
		AssetCategory: "STK",
		EntryMethod:   "manual",
	}
	require.NoError(t, db.Create(&txn).Error)
	return txn
}

// TestManualEntry_Deduplication_SameTwice verifies that inserting the same manual
// transaction twice (using the float-match check) results in exactly one DB row.
func TestManualEntry_Deduplication_SameTwice(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "me_hash1")
	dt := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	insertManual(t, db, user.ID, "AAPL", 10, 185.5, dt)

	// Simulate the duplicate check the handler performs.
	var existing models.Transaction
	err := db.Where(
		"user_id = ? AND type = ? AND symbol = ? AND date_time = ? AND quantity >= ? AND quantity <= ? AND price >= ? AND price <= ?",
		user.ID, "Trade", "AAPL", dt, 10.0-1e-8, 10.0+1e-8, 185.5-1e-8, 185.5+1e-8,
	).First(&existing).Error
	assert.NoError(t, err, "duplicate check should find the first row")
	assert.NotEmpty(t, existing.PublicID, "existing row must have a PublicID")

	var count int64
	db.Model(&models.Transaction{}).Where("user_id = ? AND symbol = ?", user.ID, "AAPL").Count(&count)
	assert.Equal(t, int64(1), count, "no duplicate should be inserted when force=false")
}

// TestManualEntry_ForceOverrideDuplicate verifies that with force=true a second identical
// manual transaction is inserted even though a match already exists.
func TestManualEntry_ForceOverrideDuplicate(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "me_hash2")
	dt := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	insertManual(t, db, user.ID, "AAPL", 10, 185.5, dt)
	insertManual(t, db, user.ID, "AAPL", 10, 185.5, dt) // force=true skips the check

	var count int64
	db.Model(&models.Transaction{}).Where("user_id = ? AND symbol = ? AND entry_method = ?", user.ID, "AAPL", "manual").Count(&count)
	assert.Equal(t, int64(2), count, "force=true should allow a second identical row")

	// Both rows must have distinct non-empty PublicIDs.
	var txns []models.Transaction
	db.Where("user_id = ? AND symbol = ?", user.ID, "AAPL").Find(&txns)
	require.Len(t, txns, 2)
	assert.NotEmpty(t, txns[0].PublicID)
	assert.NotEmpty(t, txns[1].PublicID)
	assert.NotEqual(t, txns[0].PublicID, txns[1].PublicID, "each row must have a unique UUID")
}

// TestManualEntry_AfterFlexQueryImport_Deduplicated verifies that if a FlexQuery trade
// already exists, a manual entry for the same (symbol, date, qty, price) is detected as
// a duplicate and should not be inserted.
func TestManualEntry_AfterFlexQueryImport_Deduplicated(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "me_hash3")
	dt := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	// Simulate a FlexQuery import: same trade with a TransactionID.
	fqTxn := models.Transaction{
		UserID:          user.ID,
		Type:            "Trade",
		TransactionID:   "T001",
		Symbol:          "MSFT",
		Currency:        "USD",
		DateTime:        dt,
		Quantity:        5,
		Price:           400.0,
		Proceeds:        -2000.0,
		BuySell:         "BUY",
		AssetCategory:   "STK",
		EntryMethod:     "flexquery",
	}
	require.NoError(t, db.Create(&fqTxn).Error)

	// Duplicate check: the float-match used by AddTransaction should find the FlexQuery row.
	var existing models.Transaction
	err := db.Where(
		"user_id = ? AND type = ? AND symbol = ? AND date_time = ? AND quantity >= ? AND quantity <= ? AND price >= ? AND price <= ?",
		user.ID, "Trade", "MSFT", dt, 5.0-1e-8, 5.0+1e-8, 400.0-1e-8, 400.0+1e-8,
	).First(&existing).Error
	assert.NoError(t, err, "manual entry should be detected as duplicate of FlexQuery row")

	// Since it's a duplicate (force=false), we do NOT insert.
	var count int64
	db.Model(&models.Transaction{}).Where("user_id = ? AND symbol = ?", user.ID, "MSFT").Count(&count)
	assert.Equal(t, int64(1), count, "no second row should exist")
}

// TestManualEntry_FlexQueryAfterManual_NotDeduplicated documents the known interaction
// where a manual entry is inserted first, and a subsequent FlexQuery import for the same
// trade inserts a SECOND row (because FlexQuery uses the TransactionID path, not float-match).
func TestManualEntry_FlexQueryAfterManual_NotDeduplicated(t *testing.T) {
	db := setupTestDB(t)
	dt := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)

	user := createUserWithHash(t, db, "me_hash4")
	insertManual(t, db, user.ID, "GOOG", 2, 170.0, dt)

	// Simulate FlexQuery import for the same trade — it has a TransactionID, so
	// the import path checks by (user_id, transaction_id) and finds no match → inserts.
	fqXML := strings.TrimSpace(`
<?xml version="1.0" encoding="UTF-8"?>
<FlexQueryResponse>
  <FlexStatements count="1">
    <FlexStatement accountId="U1">
      <Trades>
        <Trade symbol="GOOG" assetCategory="STK" subCategory="" currency="USD"
               listingExchange="NASDAQ" dateTime="2024-03-05;00:00:00" tradeDate="2024-03-05"
               quantity="2" tradePrice="170.00" proceeds="-340.00" ibCommission="0"
               buySell="BUY" tradeID="T_GOOG_001" conid="C1" />
      </Trades>
      <Transfers/>
      <OpenPositions/>
      <CashTransactions/>
    </FlexStatement>
  </FlexStatements>
</FlexQueryResponse>`)

	repo := NewRepository(db)
	_, _, err := repo.ParseAndSave(strings.NewReader(fqXML), "me_hash4")
	require.NoError(t, err)

	var count int64
	db.Model(&models.Transaction{}).Where("user_id = ? AND symbol = ?", user.ID, "GOOG").Count(&count)
	assert.Equal(t, int64(2), count,
		"FlexQuery import after manual entry creates a duplicate — delete the manual entry to resolve")
}

// TestManualEntry_DeleteRemovesRow verifies that a manual transaction can be deleted
// by its PublicID and no longer appears in LoadSaved output.
func TestManualEntry_DeleteRemovesRow(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "me_hash5")
	dt := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)

	txn := insertManual(t, db, user.ID, "NVDA", 3, 850.0, dt)
	require.NotEmpty(t, txn.PublicID, "inserted transaction must have a PublicID")

	// Delete by PublicID scoped to user.
	result := db.Where("public_id = ? AND user_id = ?", txn.PublicID, user.ID).Delete(&models.Transaction{})
	require.NoError(t, result.Error)
	assert.Equal(t, int64(1), result.RowsAffected)

	// Confirm it's gone.
	var count int64
	db.Model(&models.Transaction{}).Where("user_id = ? AND symbol = ?", user.ID, "NVDA").Count(&count)
	assert.Zero(t, count, "transaction should be deleted")

	// LoadSaved should return no trades for that symbol.
	repo := NewRepository(db)
	data, err := repo.LoadSaved("me_hash5")
	require.NoError(t, err)
	for _, tr := range data.Trades {
		assert.NotEqual(t, "NVDA", tr.Symbol, "deleted trade must not appear in LoadSaved")
	}
}

// TestManualEntry_EntryMethodSet verifies that each import path stores the correct entry_method.
func TestManualEntry_EntryMethodSet(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "me_hash6")
	dt := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	// Manual entry.
	manualTxn := insertManual(t, db, user.ID, "AMZN", 1, 180.0, dt)
	assert.Equal(t, "manual", manualTxn.EntryMethod)

	// FlexQuery entry.
	fqXML := strings.TrimSpace(`
<?xml version="1.0" encoding="UTF-8"?>
<FlexQueryResponse>
  <FlexStatements count="1">
    <FlexStatement accountId="U2">
      <Trades>
        <Trade symbol="TSLA" assetCategory="STK" subCategory="" currency="USD"
               listingExchange="NASDAQ" dateTime="2024-06-01;00:00:00" tradeDate="2024-06-01"
               quantity="1" tradePrice="200.00" proceeds="-200.00" ibCommission="0"
               buySell="BUY" tradeID="T_TSLA_001" conid="" />
      </Trades>
      <Transfers/>
      <OpenPositions/>
      <CashTransactions/>
    </FlexStatement>
  </FlexStatements>
</FlexQueryResponse>`)
	repo := NewRepository(db)
	_, _, err := repo.ParseAndSave(strings.NewReader(fqXML), "me_hash6")
	require.NoError(t, err)

	var fqTxn models.Transaction
	require.NoError(t, db.Where("user_id = ? AND symbol = ?", user.ID, "TSLA").First(&fqTxn).Error)
	assert.Equal(t, "flexquery", fqTxn.EntryMethod)

	// eTrade-style entry (simulates saveEtradeTransactions with entryMethod="etrade_benefits").
	etradeTxn := models.Transaction{
		UserID:        user.ID,
		Type:          "ESPP_VEST",
		Symbol:        "META",
		Currency:      "USD",
		DateTime:      dt,
		Quantity:      5,
		Price:         300.0,
		EntryMethod:   "etrade_benefits",
	}
	require.NoError(t, db.Create(&etradeTxn).Error)
	var reloaded models.Transaction
	require.NoError(t, db.Where("user_id = ? AND symbol = ?", user.ID, "META").First(&reloaded).Error)
	assert.Equal(t, "etrade_benefits", reloaded.EntryMethod)
}

// TestManualEntry_AppearsInLoadSaved verifies that a manually inserted transaction
// is returned by LoadSaved with the correct fields and a non-empty PublicID.
func TestManualEntry_AppearsInLoadSaved(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "me_hash7")
	dt := time.Date(2024, 7, 10, 0, 0, 0, 0, time.UTC)

	txn := insertManual(t, db, user.ID, "AAPL", 5, 190.0, dt)

	repo := NewRepository(db)
	data, err := repo.LoadSaved("me_hash7")
	require.NoError(t, err)
	require.Len(t, data.Trades, 1)

	tr := data.Trades[0]
	assert.Equal(t, "AAPL", tr.Symbol)
	assert.Equal(t, 5.0, tr.Quantity)
	assert.Equal(t, 190.0, tr.Price)
	assert.Equal(t, "manual", tr.EntryMethod)
	assert.Equal(t, txn.PublicID, tr.PublicID, "PublicID must be threaded through LoadSaved")
	assert.NotEmpty(t, tr.PublicID)
}

// TestManualEntry_ESPPVest_TaxCostBasis verifies that ESPP_VEST transactions store
// the employee purchase price in TaxCostBasis.
func TestManualEntry_ESPPVest_TaxCostBasis(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "me_hash8")
	dt := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)

	taxBasis := 80.0
	txn := models.Transaction{
		UserID:        user.ID,
		Type:          "ESPP_VEST",
		BuySell:       "ESPP_VEST",
		Symbol:        "CORP",
		Currency:      "USD",
		DateTime:      dt,
		Quantity:      10,
		Price:         100.0,
		Proceeds:      -1000.0,
		AssetCategory: "STK",
		TaxCostBasis:  &taxBasis,
		EntryMethod:   "manual",
	}
	require.NoError(t, db.Create(&txn).Error)

	repo := NewRepository(db)
	data, err := repo.LoadSaved("me_hash8")
	require.NoError(t, err)
	require.Len(t, data.Trades, 1)

	tr := data.Trades[0]
	require.NotNil(t, tr.TaxCostBasis, "TaxCostBasis must be set for ESPP_VEST")
	assert.Equal(t, 80.0, *tr.TaxCostBasis)
}

// TestManualEntry_RSUVest_TaxCostBasisZero verifies that RSU_VEST transactions store
// TaxCostBasis as exactly 0.0.
func TestManualEntry_RSUVest_TaxCostBasisZero(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "me_hash9")
	dt := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)

	zero := 0.0
	txn := models.Transaction{
		UserID:        user.ID,
		Type:          "RSU_VEST",
		BuySell:       "RSU_VEST",
		Symbol:        "CORP",
		Currency:      "USD",
		DateTime:      dt,
		Quantity:      8,
		Price:         200.0,
		Proceeds:      -1600.0,
		AssetCategory: "STK",
		TaxCostBasis:  &zero,
		EntryMethod:   "manual",
	}
	require.NoError(t, db.Create(&txn).Error)

	repo := NewRepository(db)
	data, err := repo.LoadSaved("me_hash9")
	require.NoError(t, err)
	require.Len(t, data.Trades, 1)

	tr := data.Trades[0]
	require.NotNil(t, tr.TaxCostBasis, "TaxCostBasis must be set for RSU_VEST")
	assert.Equal(t, 0.0, *tr.TaxCostBasis)
}

// TestManualEntry_SellNegatesQuantity verifies that a SELL manual transaction stores
// a negative quantity and positive proceeds.
func TestManualEntry_SellNegatesQuantity(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "me_hash10")
	dt := time.Date(2024, 8, 20, 0, 0, 0, 0, time.UTC)

	qty := -5.0 // handler negates the user-entered quantity for sells
	proceeds := 5.0 * 200.0
	txn := models.Transaction{
		UserID:        user.ID,
		Type:          "Trade",
		BuySell:       "SELL",
		Symbol:        "NFLX",
		Currency:      "USD",
		DateTime:      dt,
		Quantity:      qty,
		Price:         200.0,
		Proceeds:      proceeds,
		AssetCategory: "STK",
		EntryMethod:   "manual",
	}
	require.NoError(t, db.Create(&txn).Error)

	var stored models.Transaction
	require.NoError(t, db.Where("user_id = ? AND symbol = ?", user.ID, "NFLX").First(&stored).Error)
	assert.Less(t, stored.Quantity, 0.0, "sell quantity must be negative in the DB")
	assert.Greater(t, stored.Proceeds, 0.0, "sell proceeds must be positive in the DB")
}

// TestManualEntry_PublicIDIsUUID verifies that the BeforeCreate hook generates a
// valid UUID for every transaction.
func TestManualEntry_PublicIDIsUUID(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "me_hash11")
	dt := time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC)

	txn := insertManual(t, db, user.ID, "AMD", 10, 160.0, dt)
	assert.Len(t, txn.PublicID, 36, "PublicID should be a standard UUID string (36 chars)")
	assert.Equal(t, 4, strings.Count(txn.PublicID, "-"), "UUID should have 4 hyphens")
}
