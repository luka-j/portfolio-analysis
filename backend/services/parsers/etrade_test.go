package parsers

import (
	"os"
	"testing"
)

func TestParseBenefitHistory_ESPP(t *testing.T) {
	// The sample BenefitHistory.xlsx has ESPP vests
	f, err := os.Open("../../../data/BenefitHistory.xlsx")
	if err != nil {
		t.Skipf("Skipping test, sample file not found: %v", err)
	}
	defer f.Close()

	txns, err := ParseBenefitHistory(f)
	if err != nil {
		t.Fatalf("ParseBenefitHistory failed: %v", err)
	}

	if len(txns) == 0 {
		t.Fatalf("Expected at least one transaction, got none")
	}

	for _, txn := range txns {
		if txn.Type != "ESPP_VEST" && txn.Type != "RSU_VEST" {
			t.Errorf("Expected Type ESPP_VEST or RSU_VEST, got %s", txn.Type)
		}
		if txn.TaxCostBasis == nil {
			t.Errorf("Expected TaxCostBasis to be populated for %s", txn.Type)
		}
	}
}

func TestParseGainsLosses(t *testing.T) {
	// The sample G&L_Expanded.xlsx
	f, err := os.Open("../../../data/G&L_Expanded.xlsx")
	if err != nil {
		t.Skipf("Skipping test, sample file not found: %v", err)
	}
	defer f.Close()

	txns, err := ParseGainsLosses(f)
	if err != nil {
		t.Fatalf("ParseGainsLosses failed: %v", err)
	}

	if len(txns) == 0 {
		t.Fatalf("Expected at least one transaction, got none")
	}

	for _, txn := range txns {
		if txn.BuySell != "SELL" {
			t.Errorf("Expected BuySell SELL, got %s", txn.BuySell)
		}
		if txn.Quantity >= 0 {
			t.Errorf("Expected negative quantity for sells, got %f", txn.Quantity)
		}
	}
}
