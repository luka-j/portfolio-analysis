package tax

import (
	"fmt"
	"sort"
	"time"

	"gofolio-analysis/models"
	"gofolio-analysis/services/fx"
)

// Service handles calculations for Czech tax returns.
type Service struct {
	FXService *fx.Service
}

// NewService creates a new Tax service.
func NewService(fxSvc *fx.Service) *Service {
	return &Service{FXService: fxSvc}
}

type TaxTransaction struct {
	Type          string  `json:"type"`          // "ESPP_VEST", "RSU_VEST", or "SELL"
	Symbol        string  `json:"symbol"`
	Date          string  `json:"date"`          // YYYY-MM-DD
	Quantity      float64 `json:"quantity"`
	NativePrice   float64 `json:"native_price"`
	Currency      string  `json:"currency"`
	ExchangeRate  float64 `json:"exchange_rate"` // CZK rate for the primary event
	CostCZK       float64 `json:"cost_czk"`
	BenefitCZK    float64 `json:"benefit_czk"`
	BuyDate       string  `json:"buy_date,omitempty"` // For paired sells
	BuyRate       float64 `json:"buy_rate,omitempty"` // Exchange rate on buy
}

type TaxReportSection struct {
	TotalCostCZK    float64          `json:"total_cost_czk"`
	TotalBenefitCZK float64          `json:"total_benefit_czk"`
	Transactions    []TaxTransaction `json:"transactions"`
}

type TaxReportResponse struct {
	Year             int              `json:"year"`
	EmploymentIncome TaxReportSection `json:"employment_income"`
	InvestmentIncome TaxReportSection `json:"investment_income"`
}

func posKey(symbol, exchange string) string {
	return symbol
}

// GetReport generates the tax figures for the specified calendar year.
func (s *Service) GetReport(data *models.FlexQueryData, year int) (*TaxReportResponse, error) {
	resp := &TaxReportResponse{
		Year: year,
	}

	// Make a sorted copy of all trades to ensure chronological processing
	sortedTrades := make([]models.Trade, len(data.Trades))
	copy(sortedTrades, data.Trades)
	sort.Slice(sortedTrades, func(i, j int) bool {
		return sortedTrades[i].DateTime.Before(sortedTrades[j].DateTime)
	})

	// --------- Employment Income ---------
	for _, t := range sortedTrades {
		if t.DateTime.Year() != year {
			continue
		}
		if t.BuySell != "ESPP_VEST" && t.BuySell != "RSU_VEST" {
			continue
		}

		rate, err := s.FXService.GetRate(t.Currency, "CZK", t.DateTime)
		if err != nil {
			return nil, fmt.Errorf("getting fx rate for %s on %s: %w", t.Symbol, t.DateTime.Format("2006-01-02"), err)
		}

		var costNative float64
		var benefitNative float64 = t.Price * t.Quantity

		if t.BuySell == "ESPP_VEST" && t.TaxCostBasis != nil {
			costNative = *t.TaxCostBasis * t.Quantity
		} else {
			costNative = 0
		}

		tx := TaxTransaction{
			Type:         t.BuySell,
			Symbol:       t.Symbol,
			Date:         t.DateTime.Format("2006-01-02"),
			Quantity:     t.Quantity,
			NativePrice:  t.Price,
			Currency:     t.Currency,
			ExchangeRate: rate,
			CostCZK:      costNative * rate,
			BenefitCZK:   benefitNative * rate,
		}

		resp.EmploymentIncome.Transactions = append(resp.EmploymentIncome.Transactions, tx)
		resp.EmploymentIncome.TotalCostCZK += tx.CostCZK
		resp.EmploymentIncome.TotalBenefitCZK += tx.BenefitCZK
	}

	// --------- Investment Income (FIFO) ---------
	type lot struct {
		qty   float64
		price float64
		date  time.Time
		curr  string
	}
	openLots := make(map[string][]lot)

	for _, t := range sortedTrades {
		// Ignore currency routing and transfers
		if t.AssetCategory == "CASH" || (len(t.Symbol) == 7 && t.Symbol[3] == '.') || t.BuySell == "TRANSFER_IN" {
			continue
		}

		k := posKey(t.Symbol, t.ListingExchange)

		if t.Quantity > 0 {
			// Buy, Transfer, ESPP, RSU
			openLots[k] = append(openLots[k], lot{
				qty:   t.Quantity,
				price: t.Price,
				date:  t.DateTime,
				curr:  t.Currency,
			})
		} else if t.Quantity < 0 {
			// Sell
			sellQty := -t.Quantity
			sellPrice := t.Price
			sellCurrency := t.Currency

			// We need the sell rate if it falls inside the target year
			inTargetYear := (t.DateTime.Year() == year)
			var sellRate float64
			if inTargetYear {
				var err error
				sellRate, err = s.FXService.GetRate(sellCurrency, "CZK", t.DateTime)
				if err != nil {
					return nil, fmt.Errorf("getting sell fx for %s on %s: %w", t.Symbol, t.DateTime, err)
				}
			}

			// FIFO match
			lots := openLots[k]
			for sellQty > 1e-9 && len(lots) > 0 {
				matchQty := lots[0].qty
				if matchQty > sellQty {
					matchQty = sellQty
				}

				if inTargetYear {
					buyRate, err := s.FXService.GetRate(lots[0].curr, "CZK", lots[0].date)
					if err != nil {
						return nil, fmt.Errorf("getting buy fx for %s on %s: %w", t.Symbol, lots[0].date, err)
					}

					benefitCZK := matchQty * sellPrice * sellRate
					costCZK := matchQty * lots[0].price * buyRate

					tx := TaxTransaction{
						Type:         "SELL",
						Symbol:       t.Symbol,
						Date:         t.DateTime.Format("2006-01-02"),
						Quantity:     matchQty,
						NativePrice:  sellPrice,
						Currency:     sellCurrency,
						ExchangeRate: sellRate,
						CostCZK:      costCZK,
						BenefitCZK:   benefitCZK,
						BuyDate:      lots[0].date.Format("2006-01-02"),
						BuyRate:      buyRate,
					}
					resp.InvestmentIncome.Transactions = append(resp.InvestmentIncome.Transactions, tx)
					resp.InvestmentIncome.TotalCostCZK += tx.CostCZK
					resp.InvestmentIncome.TotalBenefitCZK += tx.BenefitCZK
				}

				lots[0].qty -= matchQty
				sellQty -= matchQty

				if lots[0].qty <= 1e-9 {
					lots = lots[1:]
				}
			}
			openLots[k] = lots
		}
	}

	return resp, nil
}
