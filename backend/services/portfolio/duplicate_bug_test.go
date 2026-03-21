package portfolio
import (
"testing"
"os"
flex "gofolio-analysis/services/flexquery"
)

func TestPSTGDuplicateSalesSuite(t *testing.T) {
parser := flex.NewParser(nil)
bytes, err := os.ReadFile("../../testdata/pp_export.xml")
if err != nil {
t.Fatalf("failed reading XML: %v", err)
}

data, err := parser.ParseBytes(bytes)
if err != nil {
t.Fatalf("ParseAndSave failed: %v", err)
}

svc := NewService(nil, nil) // passing nil fx and market for now to test holdings purely

holdings := svc.GetCurrentHoldings(data)

for _, h := range holdings {
if h.Symbol == "PSTG" {
t.Logf("Holding %s@%s Quantity: %f", h.Symbol, h.ListingExchange, h.Quantity)
}
}
}
