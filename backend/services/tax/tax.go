package tax

import (
	"fmt"
	"math"
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
	Type            string  `json:"type"`                       // "ESPP_VEST", "RSU_VEST", or "SELL"
	Symbol          string  `json:"symbol"`
	Date            string  `json:"date"`                       // YYYY-MM-DD
	Quantity        float64 `json:"quantity"`
	NativePrice     float64 `json:"native_price"`
	Currency        string  `json:"currency"`
	ExchangeRate    float64 `json:"exchange_rate"`              // CZK rate for the primary event
	CostCZK         float64 `json:"cost_czk"`
	BenefitCZK      float64 `json:"benefit_czk"`
	BuyDate         string  `json:"buy_date,omitempty"`         // For paired sells
	BuyRate         float64 `json:"buy_rate,omitempty"`         // Exchange rate on buy
	BuyCommission   float64 `json:"buy_commission,omitempty"`   // Pro-rated buy commission, native currency
	SellCommission  float64 `json:"sell_commission,omitempty"`  // Pro-rated sell commission, native currency
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
	if exchange == "" {
		return symbol
	}
	return symbol + "@" + exchange
}

// GetReport generates the tax figures for the specified calendar year.
// When universalRates is non-nil, its values are used for CZK conversion instead of the market data table.
func (s *Service) GetReport(data *models.FlexQueryData, year int, universalRates map[string]float64) (*TaxReportResponse, error) {
	getRate := func(currency string, date time.Time) (float64, error) {
		if universalRates != nil {
			r, ok := universalRates[currency]
			if !ok {
				return 0, fmt.Errorf("no universal exchange rate provided for currency %s", currency)
			}
			return r, nil
		}
		return s.FXService.GetRate(currency, "CZK", date, false)
	}
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

		rate, err := getRate(t.Currency, t.DateTime)
		if err != nil {
			return nil, fmt.Errorf("getting fx rate for %s on %s: %w", t.Symbol, t.DateTime.Format("2006-01-02"), err)
		}

		// For Czech tax purposes:
		//   ESPP: the full vest FMV is the benefit; the employee's purchase price is the cost.
		//         benefitNative = vestPrice × qty
		//         costNative    = taxCostBasis × qty
		//   RSU:  the full vest value is income; there is no employee cost.
		//         benefitNative = vestPrice × qty
		//         costNative    = 0
		benefitNative := t.Price * t.Quantity
		var costCZK float64
		if t.BuySell == "ESPP_VEST" && t.TaxCostBasis != nil {
			costCZK = *t.TaxCostBasis * t.Quantity * rate
		}

		tx := TaxTransaction{
			Type:         t.BuySell,
			Symbol:       t.Symbol,
			Date:         t.DateTime.Format("2006-01-02"),
			Quantity:     t.Quantity,
			NativePrice:  t.Price,
			Currency:     t.Currency,
			ExchangeRate: rate,
			CostCZK:      costCZK,
			BenefitCZK:   benefitNative * rate,
		}

		resp.EmploymentIncome.Transactions = append(resp.EmploymentIncome.Transactions, tx)
		resp.EmploymentIncome.TotalCostCZK += tx.CostCZK
		resp.EmploymentIncome.TotalBenefitCZK += tx.BenefitCZK
	}

	// --------- Investment Income (FIFO) ---------
	type lot struct {
		qty        float64
		origQty    float64 // original qty at purchase, for commission pro-rating
		price      float64
		date       time.Time
		curr       string
		commission float64 // absolute value of buy commission, native currency
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
				qty:        t.Quantity,
				origQty:    t.Quantity,
				price:      t.Price,
				date:       t.DateTime,
				curr:       t.Currency,
				commission: math.Abs(t.Commission),
			})
		} else if t.Quantity < 0 {
			// Sell
			sellQty := -t.Quantity
			sellPrice := t.Price
			sellCurrency := t.Currency

			// We need the sell rate if it falls inside the target year
			inTargetYear := (t.DateTime.Year() == year)
			var sellRate float64
			totalSellQty := sellQty // capture full sell qty for pro-rating sell commission
			if inTargetYear {
				var err error
				sellRate, err = getRate(sellCurrency, t.DateTime)
				if err != nil {
					return nil, fmt.Errorf("getting sell fx for %s on %s: %w", t.Symbol, t.DateTime, err)
				}
			}
			sellCommissionTotal := math.Abs(t.Commission)

			// FIFO match
			lots := openLots[k]
			for sellQty > 1e-9 && len(lots) > 0 {
				matchQty := lots[0].qty
				if matchQty > sellQty {
					matchQty = sellQty
				}

				if inTargetYear {
					buyRate, err := getRate(lots[0].curr, lots[0].date)
					if err != nil {
						return nil, fmt.Errorf("getting buy fx for %s on %s: %w", t.Symbol, lots[0].date, err)
					}

					// Pro-rate buy commission by the fraction of the original lot consumed
					buyComm := matchQty / lots[0].origQty * lots[0].commission
					// Pro-rate sell commission by the fraction of the total sell quantity
					sellComm := matchQty / totalSellQty * sellCommissionTotal

					benefitCZK := matchQty*sellPrice*sellRate - sellComm*sellRate
					costCZK := matchQty*lots[0].price*buyRate + buyComm*buyRate

					tx := TaxTransaction{
						Type:           "SELL",
						Symbol:         t.Symbol,
						Date:           t.DateTime.Format("2006-01-02"),
						Quantity:       matchQty,
						NativePrice:    sellPrice,
						Currency:       sellCurrency,
						ExchangeRate:   sellRate,
						CostCZK:        costCZK,
						BenefitCZK:     benefitCZK,
						BuyDate:        lots[0].date.Format("2006-01-02"),
						BuyRate:        buyRate,
						BuyCommission:  buyComm,
						SellCommission: sellComm,
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
