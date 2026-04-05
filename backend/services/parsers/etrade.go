package parsers

import (
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"portfolio-analysis/models"
)

// ParseBenefitHistory parses E*Trade BenefitHistory.xlsx for ESPP and RSU vests.
func ParseBenefitHistory(r io.Reader) ([]models.Transaction, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("opening excel: %w", err)
	}
	defer f.Close()

	if len(f.GetSheetList()) == 0 {
		return nil, fmt.Errorf("no sheets found in excel file")
	}

	var results []models.Transaction

	for _, sheetName := range f.GetSheetList() {
		rows, err := f.GetRows(sheetName)
		if err != nil || len(rows) < 2 {
			continue
		}

		headers := rows[0]
		headerMap := make(map[string]int)
		for i, h := range headers {
			headerMap[strings.TrimSpace(h)] = i
		}

		_, isESPP := headerMap["Purchase Price"]
		_, isRSU := headerMap["Settlement Type"]

		if isESPP {
			results = append(results, parseESPP(rows, headerMap)...)
		} else if isRSU {
			results = append(results, parseRSU(rows, headerMap)...)
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("did not find valid ESPP or RSU data in any sheet")
	}

	return results, nil
}

func parseESPP(rows [][]string, headerMap map[string]int) []models.Transaction {
	var results []models.Transaction

	getCol := func(row []string, name string) string {
		idx, ok := headerMap[name]
		if ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) == 0 {
			continue
		}

		if getCol(row, "Record Type") == "Purchase" {
			symbol := getCol(row, "Symbol")
			dateStr := getCol(row, "Purchase Date")
			priceStr := getCol(row, "Purchase Price")
			qtyStr := getCol(row, "Purchased Qty.")
			fmvStr := getCol(row, "Purchase Date FMV")

			date, err := parseEtradeDate(dateStr)
			if err != nil || symbol == "" {
				continue
			}

			taxCostBasis := parseFloat(priceStr)
			qty := parseFloat(qtyStr)
			fmv := parseFloat(fmvStr)

			if qty == 0 || fmv == 0 {
				continue
			}

			txn := models.Transaction{
				Type:          "ESPP_VEST",
				Symbol:        symbol,
				Currency:      "USD",
				DateTime:      date,
				Quantity:      qty,
				Price:         fmv,
				Proceeds:      -(qty * fmv),
				TaxCostBasis:  &taxCostBasis,
				AssetCategory: "STK",
				BuySell:       "ESPP_VEST",
			}
			results = append(results, txn)
		}
	}
	return results
}

func parseRSU(rows [][]string, headerMap map[string]int) []models.Transaction {
	var results []models.Transaction

	getCol := func(row []string, name string) string {
		idx, ok := headerMap[name]
		if ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	grantSymbols := make(map[string]string)
	type vestData struct {
		date string
		qty  float64
		gain float64
	}
	vestMap := make(map[string]*vestData)

	type eventRow struct {
		grantNumber string
		date        string
		qty         float64
	}
	var events []eventRow

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) == 0 {
			continue
		}

		recordType := getCol(row, "Record Type")
		grantNumber := getCol(row, "Grant Number")

		if recordType == "Grant" && grantNumber != "" {
			grantSymbols[grantNumber] = getCol(row, "Symbol")
		} else if recordType == "Vest Schedule" {
			vp := getCol(row, "Vest Period")
			date := getCol(row, "Vest Date")
			qty := parseFloat(getCol(row, "Vested Qty."))

			if grantNumber != "" && vp != "" {
				key := grantNumber + "_" + vp
				if vestMap[key] == nil {
					vestMap[key] = &vestData{}
				}
				vestMap[key].date = date
				vestMap[key].qty = qty
			}
		} else if recordType == "Tax Withholding" {
			vp := getCol(row, "Vest Period")
			gain := parseFloat(getCol(row, "Taxable Gain"))

			if grantNumber != "" && vp != "" {
				key := grantNumber + "_" + vp
				if vestMap[key] == nil {
					vestMap[key] = &vestData{}
				}
				vestMap[key].gain = gain
			}
		} else if recordType == "Event" && getCol(row, "Event Type") == "Shares released" {
			qty := parseFloat(getCol(row, "Qty. or Amount"))
			if qty > 0 {
				events = append(events, eventRow{
					grantNumber: grantNumber,
					date:        getCol(row, "Date"),
					qty:         qty,
				})
			}
		}
	}

	fmvMap := make(map[string]float64)
	for key, vd := range vestMap {
		parts := strings.Split(key, "_")
		if len(parts) == 2 && vd.date != "" && vd.qty > 0 {
			gn := parts[0]
			fmv := vd.gain / vd.qty
			
			d1, err := parseEtradeDate(vd.date)
			if err == nil {
				fmvMap[gn+"_"+d1.Format("2006-01-02")] = fmv
			}
		}
	}

	for _, ev := range events {
		t, err := parseEtradeDate(ev.date)
		if err != nil {
			continue
		}
		
		dateKey := t.Format("2006-01-02")
		fmv := fmvMap[ev.grantNumber+"_"+dateKey]
		symbol := grantSymbols[ev.grantNumber]

		if symbol == "" {
			continue
		}

		tcb := 0.0
		txn := models.Transaction{
			Type:          "RSU_VEST",
			Symbol:        symbol,
			Currency:      "USD",
			DateTime:      t,
			Quantity:      ev.qty,
			Price:         fmv,
			Proceeds:      -(ev.qty * fmv), // Simulated deposit at FMV
			TaxCostBasis:  &tcb,
			AssetCategory: "STK",
			BuySell:       "RSU_VEST",
		}
		results = append(results, txn)
	}
	return results
}

// ParseGainsLosses parses E*Trade G&L_Expanded.xlsx for sales of ESPP/RSU.
func ParseGainsLosses(r io.Reader) ([]models.Transaction, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("opening excel: %w", err)
	}
	defer f.Close()

	if len(f.GetSheetList()) == 0 {
		return nil, fmt.Errorf("no sheets found in excel file")
	}

	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil {
		return nil, fmt.Errorf("getting rows: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("excel file is empty or missing data")
	}

	headers := rows[0]
	headerMap := make(map[string]int)
	for i, h := range headers {
		headerMap[strings.TrimSpace(h)] = i
	}

	var results []models.Transaction

	getCol := func(row []string, name string) string {
		idx, ok := headerMap[name]
		if ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) == 0 {
			continue
		}

		recordType := getCol(row, "Record Type")
		if recordType == "Sell" {
			symbol := getCol(row, "Symbol")
			dateStr := getCol(row, "Date Sold")
			qtyStr := getCol(row, "Quantity")
			proceedsStr := getCol(row, "Total Proceeds")
			priceStr := getCol(row, "Proceeds Per Share")

			date, err := parseEtradeDate(dateStr)
			if err != nil {
				continue
			}

			qty := parseFloat(qtyStr)
			proceeds := parseFloat(proceedsStr)
			price := parseFloat(priceStr)

			if qty == 0 {
				continue
			}

			txn := models.Transaction{
				Type:     "Trade",
				Symbol:   symbol,
				Currency: "USD", // ETrade is typically USD
				DateTime: date,
				Quantity: -qty, // Sell is negative quantity
				Price:    price,
				Proceeds: proceeds,
				AssetCategory: "STK",
				BuySell: "SELL",
			}
			results = append(results, txn)
		}
	}

	return results, nil
}

func parseEtradeDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	formats := []string{
		"01/02/2006",
		"1/2/2006",
		"02-Jan-2006",
		"2-Jan-2006",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized etrade date format: %q", s)
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		log.Printf("Warning: failed to parse float %q: %v", s, err)
	}
	return v
}
