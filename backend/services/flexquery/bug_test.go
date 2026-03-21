package flexquery
import (
"fmt"
"testing"
"os"
"encoding/xml"
)

type MyFlexQueryResponse struct {
XMLName        xml.Name       `xml:"FlexQueryResponse"`
FlexStatements MyFlexStatements `xml:"FlexStatements"`
}

type MyFlexStatements struct {
FlexStatement MyFlexStatement `xml:"FlexStatement"`
}

type MyFlexStatement struct {
Trades           MyXmlTrades        `xml:"Trades"`
}

type MyXmlTrades struct {
Items []MyXmlTrade `xml:"Trade"`
}

type MyXmlTrade struct {
Symbol          string `xml:"symbol,attr"`
BuySell         string `xml:"buySell,attr"`
LevelOfDetail   string `xml:"levelOfDetail,attr"`
Exchange        string `xml:"exchange,attr"`
}

func TestPSTGBug(t *testing.T) {
bytes, err := os.ReadFile("../../testdata/pp_export.xml")
if err != nil {
t.Fatal(err)
}
var res MyFlexQueryResponse
err = xml.Unmarshal(bytes, &res)
if err != nil {
t.Fatal(err)
}

for _, tx := range res.FlexStatements.FlexStatement.Trades.Items {
if tx.Symbol == "PSTG" {
fmt.Printf("Raw Trade: %v %v %v\n", tx.BuySell, tx.LevelOfDetail, tx.Exchange)
}
}
}
