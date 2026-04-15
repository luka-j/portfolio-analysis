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
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Transaction{}, &models.MarketData{}, &models.CurrentPrice{}, &models.AssetFundamental{}, &models.EtfBreakdown{}, &models.CorporateActionRecord{}, &models.CashDividendRecord{}))
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
	db.Create(&models.AssetFundamental{UserID: user.ID, Symbol: "ARMY", AssetType: "Stock"})
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
	_, _, err = repo.ParseAndSave(part, "hashX")
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
	_, _, err := repo.ParseAndSave(strings.NewReader(xml), "hashBF")
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

		_, _, err = repo.ParseAndSave(part, "hashDup")
		require.NoError(t, err)
	}

	var count int64
	db.Model(&models.Transaction{}).Where("transaction_id = ?", "T999").Count(&count)
	assert.Equal(t, int64(1), count, "duplicate trade should be skipped")
}

// ---------- Helper for corporate action tests ----------

func newRepoWithDB(t *testing.T) (*Repository, models.User) {
	t.Helper()
	db := setupTestDB(t)
	repo := NewRepository(db)
	user := createUserWithHash(t, db, "corp-test-hash")
	return repo, user
}

// seedBuyTrade inserts a BUY transaction with explicit qty, price, proceeds, commission.
func seedBuyTrade(t *testing.T, db *gorm.DB, userID uint, symbol, conid string, dt time.Time, qty, price float64) models.Transaction {
	t.Helper()
	txn := models.Transaction{
		UserID:        userID,
		Type:          "Trade",
		TransactionID: fmt.Sprintf("buy-%s-%d", symbol, dt.Unix()),
		Symbol:        symbol,
		Conid:         conid,
		Currency:      "USD",
		DateTime:      dt,
		Quantity:      qty,
		Price:         price,
		Proceeds:      0,
		Commission:    0,
		BuySell:       "BUY",
		EntryMethod:   "flexquery",
	}
	require.NoError(t, db.Create(&txn).Error)
	return txn
}

func seedSellTrade(t *testing.T, db *gorm.DB, userID uint, symbol, conid string, dt time.Time, qty, price, proceeds, commission float64) models.Transaction {
	t.Helper()
	txn := models.Transaction{
		UserID:        userID,
		Type:          "Trade",
		TransactionID: fmt.Sprintf("sell-%s-%d", symbol, dt.Unix()),
		Symbol:        symbol,
		Conid:         conid,
		Currency:      "USD",
		DateTime:      dt,
		Quantity:      qty,
		Price:         price,
		Proceeds:      proceeds,
		Commission:    commission,
		BuySell:       "SELL",
		EntryMethod:   "flexquery",
	}
	require.NoError(t, db.Create(&txn).Error)
	return txn
}

// ---------- parseSplitRatio tests ----------

func TestParseSplitRatio(t *testing.T) {
	cases := []struct {
		desc string
		want float64
		ok   bool
	}{
		{"NVDA(US67066G1040) SPLIT 10 FOR 1", 10.0, true},
		{"GME(US36467W1099) 1 FOR 25 REVERSE SPLIT", 0.04, true},
		{"AAPL(US0378331005) SPLIT 4 FOR 1", 4.0, true},
		{"BYND 1 FOR 3 REVERSE SPLIT", 1.0 / 3.0, true},
		{"no ratio here", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		ratio, ok := parseSplitRatio(c.desc)
		assert.Equal(t, c.ok, ok, "ok for %q", c.desc)
		if c.ok {
			assert.InDelta(t, c.want, ratio, 1e-9, "ratio for %q", c.desc)
		}
	}
}

// ---------- parseNewSymbol tests ----------

func TestParseNewSymbol(t *testing.T) {
	cases := []struct {
		desc, want string
	}{
		{"FB(US30303M1027) CHANGE TO: META(US30303M1027)", "META"},
		{"TWTR RENAMED TO: X", "X"},
		{"GOOGL SYMBOL CHANGE TO: GOOG", "GOOG"},
		{"no change info", ""},
	}
	for _, c := range cases {
		got := parseNewSymbol(c.desc)
		assert.Equal(t, c.want, got, "for %q", c.desc)
	}
}

// ---------- IC tests ----------

func TestCorporateAction_IC_RenamesNoConidTransactions(t *testing.T) {
	repo, user := newRepoWithDB(t)
	dt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	splitDt := dt.Add(24 * time.Hour)

	// No-conid transaction — should be renamed.
	noConidTxn := seedBuyTrade(t, repo.DB, user.ID, "GOOGL", "", dt, 10, 100)
	// Conid-bearing transaction — must NOT be renamed (LoadSaved handles it dynamically).
	conidTxn := seedBuyTrade(t, repo.DB, user.ID, "GOOGL", "12345", dt, 5, 100)

	actions := []models.ParsedCorporateAction{{
		ActionID:    "ic-001",
		Type:        "IC",
		Symbol:      "GOOGL",
		DateTime:    splitDt,
		Description: "GOOGL CHANGE TO: GOOG",
	}}
	results := repo.applyCorporateActions(user.ID, actions)
	require.Len(t, results, 1)
	assert.True(t, results[0].IsNew)
	assert.Equal(t, "GOOG", results[0].NewSymbol)

	// No-conid transaction should now be named "GOOG".
	var renamed models.Transaction
	require.NoError(t, repo.DB.First(&renamed, noConidTxn.ID).Error)
	assert.Equal(t, "GOOG", renamed.Symbol)

	// Conid-bearing transaction must still be "GOOGL".
	var unchanged models.Transaction
	require.NoError(t, repo.DB.First(&unchanged, conidTxn.ID).Error)
	assert.Equal(t, "GOOGL", unchanged.Symbol)

	// One CorporateActionRecord must exist.
	var caCount int64
	repo.DB.Model(&models.CorporateActionRecord{}).Where("user_id = ? AND action_id = ?", user.ID, "ic-001").Count(&caCount)
	assert.Equal(t, int64(1), caCount)
}

func TestCorporateAction_IC_Idempotent(t *testing.T) {
	repo, user := newRepoWithDB(t)
	dt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	actions := []models.ParsedCorporateAction{{
		ActionID:    "ic-idem",
		Type:        "IC",
		Symbol:      "OLD",
		DateTime:    dt,
		Description: "OLD CHANGE TO: NEW",
	}}

	repo.applyCorporateActions(user.ID, actions)
	results := repo.applyCorporateActions(user.ID, actions)

	require.Len(t, results, 1)
	assert.False(t, results[0].IsNew)

	var caCount int64
	repo.DB.Model(&models.CorporateActionRecord{}).Where("action_id = ?", "ic-idem").Count(&caCount)
	assert.Equal(t, int64(1), caCount)
}

// ---------- FS / RS tests ----------

func TestCorporateAction_ForwardSplit_ScalesPreSplitTrades(t *testing.T) {
	repo, user := newRepoWithDB(t)
	splitDt := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	preSplit := seedBuyTrade(t, repo.DB, user.ID, "NVDA", "555", splitDt.Add(-24*time.Hour), 100, 10.0)
	postSplit := seedBuyTrade(t, repo.DB, user.ID, "NVDA", "555", splitDt.Add(24*time.Hour), 50, 20.0)

	actions := []models.ParsedCorporateAction{{
		ActionID:    "fs-001",
		Type:        "FS",
		Symbol:      "NVDA",
		Conid:       "555",
		DateTime:    splitDt,
		Description: "NVDA SPLIT 4 FOR 1",
	}}
	results := repo.applyCorporateActions(user.ID, actions)
	require.Len(t, results, 1)
	assert.True(t, results[0].IsNew)
	assert.InDelta(t, 4.0, results[0].SplitRatio, 1e-9)

	var pre, post models.Transaction
	require.NoError(t, repo.DB.First(&pre, preSplit.ID).Error)
	require.NoError(t, repo.DB.First(&post, postSplit.ID).Error)

	assert.InDelta(t, 400.0, pre.Quantity, 1e-9, "pre-split qty should be 100*4")
	assert.InDelta(t, 2.5, pre.Price, 1e-9, "pre-split price should be 10/4")
	assert.InDelta(t, 50.0, post.Quantity, 1e-9, "post-split qty unchanged")
	assert.InDelta(t, 20.0, post.Price, 1e-9, "post-split price unchanged")
}

func TestCorporateAction_ForwardSplit_DoesNotScaleProceeds(t *testing.T) {
	repo, user := newRepoWithDB(t)
	splitDt := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	txn := seedSellTrade(t, repo.DB, user.ID, "AAPL", "", splitDt.Add(-24*time.Hour), -100, 15.0, 1500.0, -5.0)

	actions := []models.ParsedCorporateAction{{
		ActionID:    "fs-proceeds",
		Type:        "FS",
		Symbol:      "AAPL",
		DateTime:    splitDt,
		Description: "AAPL SPLIT 4 FOR 1",
	}}
	repo.applyCorporateActions(user.ID, actions)

	var updated models.Transaction
	require.NoError(t, repo.DB.First(&updated, txn.ID).Error)
	assert.InDelta(t, 1500.0, updated.Proceeds, 1e-9, "proceeds must not change")
	assert.InDelta(t, -5.0, updated.Commission, 1e-9, "commission must not change")
	assert.InDelta(t, -400.0, updated.Quantity, 1e-9, "qty scaled")
	assert.InDelta(t, 3.75, updated.Price, 1e-9, "price scaled")
}

func TestCorporateAction_ReverseSplit_ScalesPreSplitTrades(t *testing.T) {
	repo, user := newRepoWithDB(t)
	splitDt := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	txn := seedBuyTrade(t, repo.DB, user.ID, "GME", "", splitDt.Add(-24*time.Hour), 100, 10.0)

	actions := []models.ParsedCorporateAction{{
		ActionID:    "rs-001",
		Type:        "RS",
		Symbol:      "GME",
		DateTime:    splitDt,
		Description: "GME 1 FOR 4 REVERSE SPLIT",
	}}
	repo.applyCorporateActions(user.ID, actions)

	var updated models.Transaction
	require.NoError(t, repo.DB.First(&updated, txn.ID).Error)
	assert.InDelta(t, 25.0, updated.Quantity, 1e-9, "qty: 100 * 0.25")
	assert.InDelta(t, 40.0, updated.Price, 1e-9, "price: 10 / 0.25")
}

func TestCorporateAction_Split_Idempotent(t *testing.T) {
	repo, user := newRepoWithDB(t)
	splitDt := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	txn := seedBuyTrade(t, repo.DB, user.ID, "TSLA", "", splitDt.Add(-24*time.Hour), 100, 10.0)

	actions := []models.ParsedCorporateAction{{
		ActionID:    "fs-idem",
		Type:        "FS",
		Symbol:      "TSLA",
		DateTime:    splitDt,
		Description: "TSLA SPLIT 4 FOR 1",
	}}

	// Apply twice.
	repo.applyCorporateActions(user.ID, actions)
	results := repo.applyCorporateActions(user.ID, actions)

	require.Len(t, results, 1)
	assert.False(t, results[0].IsNew)

	// Transaction should only be scaled once.
	var updated models.Transaction
	require.NoError(t, repo.DB.First(&updated, txn.ID).Error)
	assert.InDelta(t, 400.0, updated.Quantity, 1e-9, "qty scaled once: 100*4")

	var caCount int64
	repo.DB.Model(&models.CorporateActionRecord{}).Where("action_id = ?", "fs-idem").Count(&caCount)
	assert.Equal(t, int64(1), caCount)
}

// ---------- SD tests ----------

func TestCorporateAction_SD_InsertsStockDividendTransaction(t *testing.T) {
	repo, user := newRepoWithDB(t)
	dt := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	actions := []models.ParsedCorporateAction{{
		ActionID:    "sd-001",
		Type:        "SD",
		Symbol:      "AAPL",
		Conid:       "265598",
		Currency:    "USD",
		Quantity:    5.0,
		DateTime:    dt,
		Description: "AAPL stock dividend",
	}}
	results := repo.applyCorporateActions(user.ID, actions)
	require.Len(t, results, 1)
	assert.True(t, results[0].IsNew)

	var txn models.Transaction
	require.NoError(t, repo.DB.Where("user_id = ? AND transaction_id = ?", user.ID, "sd-001").First(&txn).Error)
	assert.Equal(t, "STOCK_DIVIDEND", txn.BuySell)
	assert.Equal(t, "AAPL", txn.Symbol)
	assert.InDelta(t, 5.0, txn.Quantity, 1e-9)
}

func TestCorporateAction_SD_Idempotent(t *testing.T) {
	repo, user := newRepoWithDB(t)
	dt := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	actions := []models.ParsedCorporateAction{{
		ActionID:    "sd-idem",
		Type:        "SD",
		Symbol:      "MSFT",
		Currency:    "USD",
		Quantity:    3.0,
		DateTime:    dt,
		Description: "MSFT stock dividend",
	}}

	repo.applyCorporateActions(user.ID, actions)
	results := repo.applyCorporateActions(user.ID, actions)

	require.Len(t, results, 1)
	assert.False(t, results[0].IsNew)

	var txnCount, caCount int64
	repo.DB.Model(&models.Transaction{}).Where("user_id = ? AND transaction_id = ?", user.ID, "sd-idem").Count(&txnCount)
	repo.DB.Model(&models.CorporateActionRecord{}).Where("action_id = ?", "sd-idem").Count(&caCount)
	assert.Equal(t, int64(1), txnCount)
	assert.Equal(t, int64(1), caCount)
}

// ---------- CD tests ----------

func TestCorporateAction_CD_InsertsCashDividend(t *testing.T) {
	repo, user := newRepoWithDB(t)
	dt := time.Date(2024, 4, 15, 0, 0, 0, 0, time.UTC)
	actions := []models.ParsedCorporateAction{{
		ActionID:    "cd-001",
		Type:        "CD",
		Symbol:      "MSFT",
		Currency:    "USD",
		Amount:      25.0,
		DateTime:    dt,
		Description: "MSFT cash dividend",
	}}
	results := repo.applyCorporateActions(user.ID, actions)
	require.Len(t, results, 1)
	assert.True(t, results[0].IsNew)

	var cd models.CashDividendRecord
	require.NoError(t, repo.DB.Where("user_id = ? AND action_id = ?", user.ID, "cd-001").First(&cd).Error)
	assert.Equal(t, "MSFT", cd.Symbol)
	assert.InDelta(t, 25.0, cd.Amount, 1e-9)
	assert.Equal(t, "USD", cd.Currency)

	var caCount int64
	repo.DB.Model(&models.CorporateActionRecord{}).Where("action_id = ?", "cd-001").Count(&caCount)
	assert.Equal(t, int64(1), caCount)
}

func TestCorporateAction_CD_Idempotent(t *testing.T) {
	repo, user := newRepoWithDB(t)
	dt := time.Date(2024, 4, 15, 0, 0, 0, 0, time.UTC)
	actions := []models.ParsedCorporateAction{{
		ActionID:    "cd-idem",
		Type:        "CD",
		Symbol:      "AAPL",
		Currency:    "USD",
		Amount:      10.0,
		DateTime:    dt,
		Description: "AAPL cash dividend",
	}}

	repo.applyCorporateActions(user.ID, actions)
	results := repo.applyCorporateActions(user.ID, actions)

	require.Len(t, results, 1)
	assert.False(t, results[0].IsNew)

	var cdCount int64
	repo.DB.Model(&models.CashDividendRecord{}).Where("action_id = ?", "cd-idem").Count(&cdCount)
	assert.Equal(t, int64(1), cdCount)
}

// ---------- IC with unparseable description (test gap 10) ----------

// TestCorporateAction_IC_UnparseableDescription verifies that an IC action whose description
// contains no recognisable "CHANGE TO:" pattern is skipped WITHOUT writing a CorporateActionRecord.
// This keeps the action retryable on the next upload rather than silently blocking it forever.
func TestCorporateAction_IC_UnparseableDescription(t *testing.T) {
	repo, user := newRepoWithDB(t)
	dt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	actions := []models.ParsedCorporateAction{{
		ActionID:    "ic-bad-desc",
		Type:        "IC",
		Symbol:      "OLD",
		DateTime:    dt,
		Description: "some garbled text with no change pattern",
	}}

	results := repo.applyCorporateActions(user.ID, actions)

	// Skipped actions do not appear in results at all.
	assert.Empty(t, results, "unparseable IC should not appear in results")

	// No CorporateActionRecord should be written — keeps the action retryable.
	var caCount int64
	repo.DB.Model(&models.CorporateActionRecord{}).Where("action_id = ?", "ic-bad-desc").Count(&caCount)
	assert.Equal(t, int64(0), caCount, "no record should be written for an unparseable IC")

	// A second call with the same action should behave identically (not blocked).
	results2 := repo.applyCorporateActions(user.ID, actions)
	assert.Empty(t, results2)
	repo.DB.Model(&models.CorporateActionRecord{}).Where("action_id = ?", "ic-bad-desc").Count(&caCount)
	assert.Equal(t, int64(0), caCount)
}

// TestCorporateAction_FS_UnparseableDescription verifies the same retry behaviour for a
// forward split whose description contains no recognisable ratio.
func TestCorporateAction_FS_UnparseableDescription(t *testing.T) {
	repo, user := newRepoWithDB(t)
	dt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	actions := []models.ParsedCorporateAction{{
		ActionID:    "fs-bad-desc",
		Type:        "FS",
		Symbol:      "TSLA",
		DateTime:    dt,
		Description: "no ratio information here",
	}}

	results := repo.applyCorporateActions(user.ID, actions)
	assert.Empty(t, results)

	var caCount int64
	repo.DB.Model(&models.CorporateActionRecord{}).Where("action_id = ?", "fs-bad-desc").Count(&caCount)
	assert.Equal(t, int64(0), caCount)
}

// ---------- FS with mixed conid / no-conid trades (test gap 9) ----------

// TestCorporateAction_ForwardSplit_MixedConidAndNoConid verifies that when a FS action carries a
// conid, it scales both conid-bearing trades and no-conid trades for the same symbol, while leaving
// no-conid trades for a different symbol untouched.
func TestCorporateAction_ForwardSplit_MixedConidAndNoConid(t *testing.T) {
	repo, user := newRepoWithDB(t)
	splitDt := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	preDt := splitDt.Add(-24 * time.Hour)

	// Trade 1: conid-bearing, same security — must be scaled.
	conidTrade := seedBuyTrade(t, repo.DB, user.ID, "NVDA", "555", preDt, 100, 10.0)
	// Trade 2: no conid, same symbol — must also be scaled (symbol fallback).
	noConidTrade := seedBuyTrade(t, repo.DB, user.ID, "NVDA", "", preDt, 50, 10.0)
	// Trade 3: no conid, different symbol — must NOT be touched.
	otherTrade := seedBuyTrade(t, repo.DB, user.ID, "AAPL", "", preDt, 20, 15.0)

	actions := []models.ParsedCorporateAction{{
		ActionID:    "fs-mixed",
		Type:        "FS",
		Symbol:      "NVDA",
		Conid:       "555",
		DateTime:    splitDt,
		Description: "NVDA SPLIT 4 FOR 1",
	}}
	results := repo.applyCorporateActions(user.ID, actions)
	require.Len(t, results, 1)
	assert.True(t, results[0].IsNew)
	assert.InDelta(t, 4.0, results[0].SplitRatio, 1e-9)

	var t1, t2, t3 models.Transaction
	require.NoError(t, repo.DB.First(&t1, conidTrade.ID).Error)
	require.NoError(t, repo.DB.First(&t2, noConidTrade.ID).Error)
	require.NoError(t, repo.DB.First(&t3, otherTrade.ID).Error)

	// Conid-bearing NVDA trade: scaled.
	assert.InDelta(t, 400.0, t1.Quantity, 1e-9, "conid trade qty should be scaled 4×")
	assert.InDelta(t, 2.5, t1.Price, 1e-9, "conid trade price should be divided by 4")

	// No-conid NVDA trade: also scaled via symbol fallback.
	assert.InDelta(t, 200.0, t2.Quantity, 1e-9, "no-conid NVDA trade qty should be scaled 4×")
	assert.InDelta(t, 2.5, t2.Price, 1e-9, "no-conid NVDA trade price should be divided by 4")

	// AAPL trade: untouched.
	assert.InDelta(t, 20.0, t3.Quantity, 1e-9, "AAPL trade should not be scaled")
	assert.InDelta(t, 15.0, t3.Price, 1e-9, "AAPL price should not change")
}

// ---------- ParseAndSave end-to-end with inline XML (test gap 8) ----------

// TestParseAndSave_CorporateAction_ForwardSplit feeds a full FlexQuery XML document
// containing a trade and a FS CorporateAction through ParseAndSave and verifies that
// the pre-split trade seeded in the DB is scaled and the import result is populated.
func TestParseAndSave_CorporateAction_ForwardSplit(t *testing.T) {
	db := setupTestDB(t)
	user := createUserWithHash(t, db, "hash-ca-fs")

	splitDt := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	preDt := splitDt.Add(-24 * time.Hour)

	// Seed a pre-split buy trade directly in the DB (simulating a prior upload).
	preTrade := models.Transaction{
		UserID:        user.ID,
		Type:          "Trade",
		TransactionID: "T-presplit",
		Symbol:        "NVDA",
		Conid:         "555",
		Currency:      "USD",
		DateTime:      preDt,
		Quantity:      100,
		Price:         10.0,
		BuySell:       "BUY",
		EntryMethod:   "flexquery",
	}
	require.NoError(t, db.Create(&preTrade).Error)

	xml := strings.TrimSpace(`
<?xml version="1.0" encoding="UTF-8"?>
<FlexQueryResponse>
  <FlexStatements count="1">
    <FlexStatement accountId="U999">
      <Trades/>
      <Transfers/>
      <OpenPositions/>
      <CashTransactions/>
      <CorporateActions>
        <CorporateAction type="FS" symbol="NVDA" conid="555" currency="USD"
                         quantity="300" amount="0"
                         dateTime="2024-06-01" reportDate="2024-06-01"
                         description="NVDA SPLIT 4 FOR 1"
                         actionID="ca-fs-xml-001" />
      </CorporateActions>
    </FlexStatement>
  </FlexStatements>
</FlexQueryResponse>`)

	repo := NewRepository(db)
	_, result, err := repo.ParseAndSave(strings.NewReader(xml), "hash-ca-fs")
	require.NoError(t, err)

	require.Len(t, result.CorporateActions, 1)
	ca := result.CorporateActions[0]
	assert.Equal(t, "FS", ca.Type)
	assert.Equal(t, "NVDA", ca.Symbol)
	assert.True(t, ca.IsNew)
	assert.InDelta(t, 4.0, ca.SplitRatio, 1e-9)

	// Pre-split trade should have been scaled 4×.
	var updated models.Transaction
	require.NoError(t, db.First(&updated, preTrade.ID).Error)
	assert.InDelta(t, 400.0, updated.Quantity, 1e-9, "pre-split qty should be 100*4")
	assert.InDelta(t, 2.5, updated.Price, 1e-9, "pre-split price should be 10/4")

	// CorporateActionRecord must exist for idempotency.
	var caCount int64
	db.Model(&models.CorporateActionRecord{}).Where("action_id = ?", "ca-fs-xml-001").Count(&caCount)
	assert.Equal(t, int64(1), caCount)

	// Re-upload the same XML — action should be reported as already applied (IsNew=false).
	_, result2, err := repo.ParseAndSave(strings.NewReader(xml), "hash-ca-fs")
	require.NoError(t, err)
	require.Len(t, result2.CorporateActions, 1)
	assert.False(t, result2.CorporateActions[0].IsNew, "second upload should report IsNew=false")
}

// ---------- LoadSaved cash dividends test ----------

func TestLoadSaved_PopulatesCashDividends(t *testing.T) {
	repo, user := newRepoWithDB(t)
	dt := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)

	// Seed a CashDividendRecord directly.
	cd := models.CashDividendRecord{
		UserID:      user.ID,
		ActionID:    "cd-load-001",
		Symbol:      "NVDA",
		Currency:    "USD",
		Amount:      42.0,
		DateTime:    dt,
		Description: "NVDA dividend",
	}
	require.NoError(t, repo.DB.Create(&cd).Error)

	data, err := repo.LoadSaved(user.TokenHash)
	require.NoError(t, err)
	require.Len(t, data.CashDividends, 1)
	assert.Equal(t, "NVDA", data.CashDividends[0].Symbol)
	assert.InDelta(t, 42.0, data.CashDividends[0].Amount, 1e-9)
}
