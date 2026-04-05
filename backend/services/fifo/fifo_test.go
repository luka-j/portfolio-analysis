package fifo

import (
	"testing"
	"time"

	"portfolio-analysis/models"
)

// helper builds a Trade with sensible defaults.
func mkTrade(buySell string, qty, price, commission float64, dt time.Time) models.Trade {
	return models.Trade{
		BuySell:    buySell,
		Quantity:   qty,
		Price:      price,
		Commission: commission,
		DateTime:   dt,
		Currency:   "USD",
	}
}

var (
	t1 = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 = time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC)
	t3 = time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC)
)

// TestMatchSimpleBuySell verifies a single buy followed by a full sell.
func TestMatchSimpleBuySell(t *testing.T) {
	trades := []models.Trade{
		mkTrade("BUY", 10, 100, 5, t1),
		mkTrade("SELL", -10, 150, 3, t2),
	}

	openLots, matched := Match(trades)

	if len(openLots) != 0 {
		t.Fatalf("expected 0 open lots, got %d", len(openLots))
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 matched sell, got %d", len(matched))
	}
	m := matched[0]
	if m.Qty != 10 {
		t.Errorf("matched qty: want 10, got %v", m.Qty)
	}
	if m.CostPrice != 100 {
		t.Errorf("cost price: want 100, got %v", m.CostPrice)
	}
	if m.SellPrice != 150 {
		t.Errorf("sell price: want 150, got %v", m.SellPrice)
	}
	if m.Comm != 3 {
		t.Errorf("commission: want 3, got %v", m.Comm)
	}
}

// TestMatchPartialSell verifies a partial sell leaves residual open lots.
func TestMatchPartialSell(t *testing.T) {
	trades := []models.Trade{
		mkTrade("BUY", 10, 100, 0, t1),
		mkTrade("SELL", -4, 120, 0, t2),
	}

	openLots, matched := Match(trades)

	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Qty != 4 {
		t.Errorf("matched qty: want 4, got %v", matched[0].Qty)
	}
	if len(openLots) != 1 {
		t.Fatalf("expected 1 open lot, got %d", len(openLots))
	}
	if openLots[0].Qty != 6 {
		t.Errorf("remaining qty: want 6, got %v", openLots[0].Qty)
	}
}

// TestMatchMultipleLotsFIFO verifies two buy lots are consumed in chronological order.
func TestMatchMultipleLotsFIFO(t *testing.T) {
	trades := []models.Trade{
		mkTrade("BUY", 5, 100, 0, t1),
		mkTrade("BUY", 5, 200, 0, t2),
		mkTrade("SELL", -7, 300, 0, t3),
	}

	openLots, matched := Match(trades)

	// 5 from first lot + 2 from second lot -> 2 MatchedSell chunks
	if len(matched) != 2 {
		t.Fatalf("expected 2 matched chunks, got %d", len(matched))
	}
	if matched[0].Qty != 5 || matched[0].CostPrice != 100 {
		t.Errorf("chunk 0: want qty=5 cost=100, got qty=%v cost=%v", matched[0].Qty, matched[0].CostPrice)
	}
	if matched[1].Qty != 2 || matched[1].CostPrice != 200 {
		t.Errorf("chunk 1: want qty=2 cost=200, got qty=%v cost=%v", matched[1].Qty, matched[1].CostPrice)
	}
	// Remaining open: 3 shares at 200
	if len(openLots) != 1 || openLots[0].Qty != 3 {
		t.Errorf("open lots: want 1 lot of 3, got %+v", openLots)
	}
}

// TestMatchCommissionOnFirstChunkOnly verifies commission is allocated to the first
// matched chunk and zeroed for subsequent chunks from the same sell.
func TestMatchCommissionOnFirstChunkOnly(t *testing.T) {
	trades := []models.Trade{
		mkTrade("BUY", 5, 100, 0, t1),
		mkTrade("BUY", 5, 200, 0, t2),
		mkTrade("SELL", -8, 300, 10, t3), // commission = 10
	}

	_, matched := Match(trades)

	if matched[0].Comm != 10 {
		t.Errorf("first chunk commission: want 10, got %v", matched[0].Comm)
	}
	for i, m := range matched[1:] {
		if m.Comm != 0 {
			t.Errorf("chunk %d commission: want 0, got %v", i+1, m.Comm)
		}
	}
}

// TestMatchTransferInExcluded verifies TRANSFER_IN trades are not added to open lots.
func TestMatchTransferInExcluded(t *testing.T) {
	trades := []models.Trade{
		{BuySell: "TRANSFER_IN", Quantity: 10, Price: 50, DateTime: t1, Currency: "USD"},
		mkTrade("BUY", 5, 100, 0, t2),
		mkTrade("SELL", -5, 150, 0, t3),
	}

	openLots, matched := Match(trades)

	// Only the BUY should be open-lot-eligible; the SELL should match just that.
	if len(matched) != 1 || matched[0].CostPrice != 100 {
		t.Errorf("unexpected matches: %+v", matched)
	}
	if len(openLots) != 0 {
		t.Errorf("expected 0 open lots after full match, got %d", len(openLots))
	}
}

// TestMatchNoBuys verifies selling with no open lots produces no matches.
func TestMatchNoBuys(t *testing.T) {
	trades := []models.Trade{
		mkTrade("SELL", -5, 100, 0, t1),
	}

	openLots, matched := Match(trades)

	if len(openLots) != 0 {
		t.Errorf("expected 0 open lots, got %d", len(openLots))
	}
	if len(matched) != 0 {
		t.Errorf("expected 0 matched sells, got %d", len(matched))
	}
}

// TestMatchNoTrades verifies empty input returns empty output.
func TestMatchNoTrades(t *testing.T) {
	openLots, matched := Match(nil)
	if len(openLots) != 0 || len(matched) != 0 {
		t.Error("expected empty results for nil trades")
	}
}
