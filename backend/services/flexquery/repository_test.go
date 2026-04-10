package flexquery

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"portfolio-analysis/models"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:fq_test_%d?mode=memory&cache=shared", time.Now().UnixNano())),
		&gorm.Config{},
	)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Transaction{}, &models.MarketData{}, &models.CurrentPrice{}, &models.AssetFundamental{}, &models.EtfBreakdown{}))
	return db
}

// seedTrade inserts a Transaction of type "Trade" directly into the DB.
func seedTrade(t *testing.T, db *gorm.DB, userID uint, symbol, conid, exchange string, dt time.Time) {
	t.Helper()
	txn := models.Transaction{
		UserID:          userID,
		Type:            "Trade",
		TransactionID:   fmt.Sprintf("%s-%s-%d", symbol, conid, dt.Unix()),
		Symbol:          symbol,
		Conid:           conid,
		Currency:        "USD",
		ListingExchange: exchange,
		DateTime:        dt,
		Quantity:        10,
		Price:           100,
		BuySell:         "BUY",
		AssetCategory:   "STK",
	}
	require.NoError(t, db.Create(&txn).Error)
}

// seedTypedTrade inserts a Transaction with an arbitrary type.
func seedTypedTrade(t *testing.T, db *gorm.DB, userID uint, txnType, symbol, conid, exchange string, dt time.Time) {
	t.Helper()
	txn := models.Transaction{
		UserID:          userID,
		Type:            txnType,
		TransactionID:   fmt.Sprintf("%s-%s-%s-%d", txnType, symbol, conid, dt.Unix()),
		Symbol:          symbol,
		Conid:           conid,
		Currency:        "USD",
		ListingExchange: exchange,
		DateTime:        dt,
		Quantity:        5,
		Price:           50,
		BuySell:         txnType,
		AssetCategory:   "STK",
	}
	require.NoError(t, db.Create(&txn).Error)
}

func createUserWithHash(t *testing.T, db *gorm.DB, hash string) models.User {
	t.Helper()
	user := models.User{TokenHash: hash}
	require.NoError(t, db.Create(&user).Error)
	return user
}

// TestLoadSaved_SymbolNormalisedByConid verifies that when two trades share the same
// conid but have different symbols, both are returned with the latest symbol.
func TestLoadSaved_SymbolNormalisedByConid(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "hash1")

	older := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)

	seedTrade(t, db, user.ID, "IBTF.DE", "C1", "XETRA", older)
	seedTrade(t, db, user.ID, "IBTF", "C1", "XETRA", newer)

	repo := NewRepository(db)
	data, err := repo.LoadSaved("hash1")
	require.NoError(t, err)
	require.Len(t, data.Trades, 2)

	for _, tr := range data.Trades {
		assert.Equal(t, "IBTF", tr.Symbol, "both trades should use the latest symbol for conid C1")
	}
}

// TestLoadSaved_NoConidFallsBackToSymbol verifies that trades without a conid keep
// their original symbol unchanged.
func TestLoadSaved_NoConidFallsBackToSymbol(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "hash2")

	dt := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	seedTrade(t, db, user.ID, "AAPL", "", "NASDAQ", dt)
	seedTrade(t, db, user.ID, "MSFT", "", "NASDAQ", dt)

	repo := NewRepository(db)
	data, err := repo.LoadSaved("hash2")
	require.NoError(t, err)
	require.Len(t, data.Trades, 2)

	syms := make(map[string]bool)
	for _, tr := range data.Trades {
		syms[tr.Symbol] = true
	}
	assert.True(t, syms["AAPL"])
	assert.True(t, syms["MSFT"])
}

// TestLoadSaved_EtradeEnrichedByConid verifies that an eTrade RSU_VEST transaction
// with no conid is normalised to the canonical IBKR symbol when the mapping is unambiguous.
func TestLoadSaved_EtradeEnrichedByConid(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "hash3")

	older := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	vest := time.Date(2022, 6, 1, 0, 0, 0, 0, time.UTC)

	// IBKR trades: same conid, symbol renamed OLD → NEW
	seedTrade(t, db, user.ID, "OLD", "C1", "NASDAQ", older)
	seedTrade(t, db, user.ID, "NEW", "C1", "NASDAQ", newer)
	// eTrade RSU vest: no conid, original symbol
	seedTypedTrade(t, db, user.ID, "RSU_VEST", "OLD", "", "", vest)

	repo := NewRepository(db)
	data, err := repo.LoadSaved("hash3")
	require.NoError(t, err)
	require.Len(t, data.Trades, 3)

	for _, tr := range data.Trades {
		assert.Equal(t, "NEW", tr.Symbol,
			"all trades (including eTrade RSU_VEST) should use canonical symbol NEW")
	}
}

// TestLoadSaved_EtradeAmbiguousConidNotNormalised verifies that when a symbol maps to
// multiple conids (same ticker on different exchanges), eTrade trades are not normalised.
func TestLoadSaved_EtradeAmbiguousConidNotNormalised(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "hash4")

	dt := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	// Two IBKR trades with the same symbol but different conids (different exchanges)
	seedTrade(t, db, user.ID, "XYZ", "C_NYSE", "NYSE", dt)
	seedTrade(t, db, user.ID, "XYZ", "C_LSE", "LSE", dt)
	// eTrade RSU with same symbol
	seedTypedTrade(t, db, user.ID, "RSU_VEST", "XYZ", "", "", dt)

	repo := NewRepository(db)
	data, err := repo.LoadSaved("hash4")
	require.NoError(t, err)

	// Find the RSU_VEST trade
	var rstTrade *models.Trade
	for i := range data.Trades {
		if data.Trades[i].BuySell == "RSU_VEST" {
			rstTrade = &data.Trades[i]
			break
		}
	}
	require.NotNil(t, rstTrade)
	assert.Equal(t, "XYZ", rstTrade.Symbol, "ambiguous conid mapping — symbol should be unchanged")
}

// TestUpdateSymbolMapping_UpdatesByConid verifies that updating the Yahoo symbol
// for the canonical ticker also updates historical rows that stored the old ticker.
func TestUpdateSymbolMapping_UpdatesByConid(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "hash5")

	older := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	mid := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	seedTrade(t, db, user.ID, "OLD", "C1", "NASDAQ", older)
	seedTrade(t, db, user.ID, "OLD", "C1", "NASDAQ", mid)
	seedTrade(t, db, user.ID, "NEW", "C1", "NASDAQ", newer)

	repo := NewRepository(db)
	err := repo.UpdateSymbolMapping("hash5", "NEW", "NASDAQ", "NEW.YAHOO")
	require.NoError(t, err)

	var txns []models.Transaction
	db.Where("user_id = ?", user.ID).Find(&txns)

	for _, txn := range txns {
		assert.Equal(t, "NEW.YAHOO", txn.YahooSymbol,
			"all rows sharing conid C1 should have YahooSymbol updated, symbol=%s", txn.Symbol)
	}
}

// TestUpdateSymbolMapping_ScopedByExchange verifies that a mapping for a symbol on one
// exchange does not overwrite the yahoo_symbol of the same ticker on a different exchange.
func TestUpdateSymbolMapping_ScopedByExchange(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "hash6")

	dt := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	// Same ticker, different exchanges, different conids.
	seedTrade(t, db, user.ID, "VUAA", "C_LSE", "LSEETF", dt)
	seedTrade(t, db, user.ID, "VUAA", "C_XTRA", "XTRA", dt)

	repo := NewRepository(db)
	// Map VUAA only on LSEETF.
	err := repo.UpdateSymbolMapping("hash6", "VUAA", "LSEETF", "VUAA.L")
	require.NoError(t, err)

	var lse, xtra models.Transaction
	db.Where("user_id = ? AND conid = ?", user.ID, "C_LSE").First(&lse)
	db.Where("user_id = ? AND conid = ?", user.ID, "C_XTRA").First(&xtra)

	assert.Equal(t, "VUAA.L", lse.YahooSymbol, "LSEETF row should be mapped")
	assert.Equal(t, "", xtra.YahooSymbol, "XTRA row must NOT be affected by a mapping scoped to LSEETF")
}

// TestUpdateSymbolMapping_PurgesOldCache verifies that switching the Yahoo symbol for a
// broker ticker deletes cached market data, current prices, asset fundamentals, and ETF
// breakdowns that were fetched under the old effective ticker.
func TestUpdateSymbolMapping_PurgesOldCache(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "hash7")

	dt := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	seedTrade(t, db, user.ID, "ARMY", "C1", "LSE", dt)

	// Seed stale cache data under the broker symbol "ARMY" (fetched before mapping existed).
	db.Create(&models.MarketData{Symbol: "ARMY", Date: dt, Volume: 100})
	db.Create(&models.CurrentPrice{Symbol: "ARMY", Price: 10.0, FetchedAt: dt})
	db.Create(&models.AssetFundamental{Symbol: "ARMY", AssetType: "Stock"})
	db.Create(&models.EtfBreakdown{FundSymbol: "ARMY", Dimension: "sector", Label: "Defense", Weight: 1.0})

	repo := NewRepository(db)
	err := repo.UpdateSymbolMapping("hash7", "ARMY", "LSE", "ARMY.L")
	require.NoError(t, err)

	var mdCount, cpCount, afCount, etfCount int64
	db.Model(&models.MarketData{}).Where("symbol = ?", "ARMY").Count(&mdCount)
	db.Model(&models.CurrentPrice{}).Where("symbol = ?", "ARMY").Count(&cpCount)
	db.Model(&models.AssetFundamental{}).Where("symbol = ?", "ARMY").Count(&afCount)
	db.Model(&models.EtfBreakdown{}).Where("fund_symbol = ?", "ARMY").Count(&etfCount)

	assert.Zero(t, mdCount, "market_data for old ticker should be purged")
	assert.Zero(t, cpCount, "current_prices for old ticker should be purged")
	assert.Zero(t, afCount, "asset_fundamentals for old ticker should be purged")
	assert.Zero(t, etfCount, "etf_breakdowns for old ticker should be purged")
}

// TestUpdateSymbolMapping_NoPurgeWhenTickerUnchanged verifies that updating to the
// same effective Yahoo symbol does not delete any cached data.
func TestUpdateSymbolMapping_NoPurgeWhenTickerUnchanged(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "hash8")

	dt := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	seedTrade(t, db, user.ID, "AAPL", "C2", "NASDAQ", dt)

	// Seed cache data for AAPL.L (the existing Yahoo mapping).
	db.Create(&models.MarketData{Symbol: "AAPL.L", Date: dt, Volume: 50})

	// First mapping: AAPL → AAPL.L
	repo := NewRepository(db)
	require.NoError(t, repo.UpdateSymbolMapping("hash8", "AAPL", "NASDAQ", "AAPL.L"))

	// Second call with identical Yahoo symbol — should not purge.
	require.NoError(t, repo.UpdateSymbolMapping("hash8", "AAPL", "NASDAQ", "AAPL.L"))

	var mdCount int64
	db.Model(&models.MarketData{}).Where("symbol = ?", "AAPL.L").Count(&mdCount)
	assert.Equal(t, int64(1), mdCount, "market_data for unchanged ticker must not be purged")
}

// TestParseAndSave_StoresConid verifies that ParseAndSave persists the conid
// attribute from a FlexQuery XML trade into the database.
func TestParseAndSave_StoresConid(t *testing.T) {
	db := setupTestDB(t)

	xml := strings.TrimSpace(`
<?xml version="1.0" encoding="UTF-8"?>
<FlexQueryResponse>
  <FlexStatements count="1">
    <FlexStatement accountId="U123456">
      <Trades>
        <Trade symbol="AAPL" assetCategory="STK" subCategory="" currency="USD"
               listingExchange="NASDAQ" dateTime="2023-01-10;10:00:00" tradeDate="2023-01-10"
               quantity="10" tradePrice="150.00" proceeds="-1500.00" ibCommission="-1.00"
               buySell="BUY" tradeID="T001" conid="265598" />
      </Trades>
      <Transfers/>
      <OpenPositions/>
      <CashTransactions/>
    </FlexStatement>
  </FlexStatements>
</FlexQueryResponse>`)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "flex.xml")
	require.NoError(t, err)
	_, err = fw.Write([]byte(xml))
	require.NoError(t, err)
	w.Close()

	mr := multipart.NewReader(&buf, w.Boundary())
	part, err := mr.NextPart()
	require.NoError(t, err)

	repo := NewRepository(db)
	_, err = repo.ParseAndSave(part, "hashX")
	require.NoError(t, err)

	var txn models.Transaction
	err = db.Where("transaction_id = ?", "T001").First(&txn).Error
	require.NoError(t, err)
	assert.Equal(t, "265598", txn.Conid)
}

// TestParseAndSave_BackfillsConidOnReupload verifies that re-uploading a FlexQuery
// retroactively fills in the conid on old rows that were imported without it.
func TestParseAndSave_BackfillsConidOnReupload(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "hashBF")

	// Simulate an old import: trade exists with no conid.
	old := models.Transaction{
		UserID:          user.ID,
		Type:            "Trade",
		TransactionID:   "T100",
		Symbol:          "IBTF.DE",
		Conid:           "", // imported before conid support
		Currency:        "EUR",
		ListingExchange: "XETRA",
		DateTime:        time.Date(2022, 6, 1, 10, 0, 0, 0, time.UTC),
		Quantity:        10,
		Price:           50,
		Proceeds:        -500,
		Commission:      -1,
		BuySell:         "BUY",
		AssetCategory:   "ETF",
	}
	require.NoError(t, db.Create(&old).Error)

	// Re-upload a FlexQuery that now includes conid for the same trade.
	xml := strings.TrimSpace(`
<?xml version="1.0" encoding="UTF-8"?>
<FlexQueryResponse>
  <FlexStatements count="1">
    <FlexStatement accountId="U999">
      <Trades>
        <Trade symbol="IBTF" assetCategory="STK" subCategory="ETF" currency="EUR"
               listingExchange="XETRA" dateTime="2022-06-01;10:00:00" tradeDate="2022-06-01"
               quantity="10" tradePrice="50.00" proceeds="-500.00" ibCommission="-1.00"
               buySell="BUY" tradeID="T100" conid="C_IBTF" />
      </Trades>
      <Transfers/>
      <OpenPositions/>
      <CashTransactions/>
    </FlexStatement>
  </FlexStatements>
</FlexQueryResponse>`)

	repo := NewRepository(db)
	_, err := repo.ParseAndSave(strings.NewReader(xml), "hashBF")
	require.NoError(t, err)

	// Should still be exactly one row (deduplicated), now with conid backfilled.
	var count int64
	db.Model(&models.Transaction{}).Where("transaction_id = ?", "T100").Count(&count)
	assert.Equal(t, int64(1), count, "should not create a duplicate")

	var txn models.Transaction
	require.NoError(t, db.Where("transaction_id = ?", "T100").First(&txn).Error)
	assert.Equal(t, "C_IBTF", txn.Conid, "conid should be backfilled")
	assert.Equal(t, "IBTF", txn.Symbol, "symbol should be updated to the current ticker")
}

// TestLoadSaved_DeduplicationByTransactionID verifies that re-importing the same
// trade (same tradeID) does not create duplicate rows, even with conid present.
func TestLoadSaved_DeduplicationByTransactionID(t *testing.T) {
	db := setupTestDB(t)

	xml := strings.TrimSpace(`
<?xml version="1.0" encoding="UTF-8"?>
<FlexQueryResponse>
  <FlexStatements count="1">
    <FlexStatement accountId="U123">
      <Trades>
        <Trade symbol="MSFT" assetCategory="STK" subCategory="" currency="USD"
               listingExchange="NASDAQ" dateTime="2023-03-01;09:30:00" tradeDate="2023-03-01"
               quantity="5" tradePrice="280.00" proceeds="-1400.00" ibCommission="-1.00"
               buySell="BUY" tradeID="T999" conid="272093" />
      </Trades>
      <Transfers/>
      <OpenPositions/>
      <CashTransactions/>
    </FlexStatement>
  </FlexStatements>
</FlexQueryResponse>`)

	repo := NewRepository(db)

	// Import twice.
	for i := 0; i < 2; i++ {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fw, err := w.CreateFormFile("file", "flex.xml")
		require.NoError(t, err)
		_, err = fw.Write([]byte(xml))
		require.NoError(t, err)
		w.Close()

		mr := multipart.NewReader(&buf, w.Boundary())
		part, err := mr.NextPart()
		require.NoError(t, err)

		_, err = repo.ParseAndSave(part, "hashDup")
		require.NoError(t, err)
	}

	var count int64
	db.Model(&models.Transaction{}).Where("transaction_id = ?", "T999").Count(&count)
	assert.Equal(t, int64(1), count, "duplicate trade should be skipped")
}
