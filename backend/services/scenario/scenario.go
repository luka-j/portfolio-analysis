package scenario

import (
	"fmt"
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/market"
)

// BaseMode controls whether the scenario starts from real portfolio data or an empty state.
type BaseMode string

const (
	BaseModeReal     BaseMode = "real"
	BaseModeEmpty    BaseMode = "empty"
	BaseModeRedirect BaseMode = "redirect"
)

// AdjustmentAction describes what to do with an existing position.
type AdjustmentAction string

const (
	ActionSellQty AdjustmentAction = "sell_qty" // sell a specific quantity
	ActionSellPct AdjustmentAction = "sell_pct" // sell a percentage (0–100) of the position
	ActionSellAll AdjustmentAction = "sell_all" // sell the entire position
	ActionBuy     AdjustmentAction = "buy"      // buy shares worth Value units of Currency
)

// BasketMode determines how basket items are sized.
type BasketMode string

const (
	BasketModeQuantity BasketMode = "quantity"
	BasketModeWeight   BasketMode = "weight"
)

// RebalanceMode controls how a backtest rebalances to target weights.
type RebalanceMode string

const (
	RebalanceModeNone      RebalanceMode = "none"
	RebalanceModeMonthly   RebalanceMode = "monthly"
	RebalanceModeQuarterly RebalanceMode = "quarterly"
	RebalanceModeAnnually  RebalanceMode = "annually"
	RebalanceModeThreshold RebalanceMode = "threshold"
)

// ContributionCadence controls periodic DCA contributions in a backtest.
type ContributionCadence string

const (
	ContributionNone      ContributionCadence = "none"
	ContributionMonthly   ContributionCadence = "monthly"
	ContributionQuarterly ContributionCadence = "quarterly"
	ContributionAnnually  ContributionCadence = "annually"
)

// ScenarioSpec describes how to construct a counterfactual portfolio.
// It is stored as JSON in the database and sent from the frontend.
type ScenarioSpec struct {
	Base        BaseMode        `json:"base"`
	BaseAsOf    *DateOnly       `json:"base_as_of,omitempty"`
	Adjustments []Adjustment    `json:"adjustments,omitempty"`
	Basket      *Basket         `json:"basket,omitempty"`
	Backtest    *BacktestConfig `json:"backtest,omitempty"`
}

// Adjustment modifies an existing position in the base portfolio.
type Adjustment struct {
	Symbol   string           `json:"symbol"`
	Exchange string           `json:"exchange,omitempty"`
	Action   AdjustmentAction `json:"action"`
	// Value semantics depend on Action:
	//   sell_qty: number of shares to sell
	//   sell_pct: percentage of position to sell (0–100)
	//   sell_all: ignored
	//   buy:      total cost in Currency to spend
	Value    float64   `json:"value,omitempty"`
	Date     *DateOnly `json:"date,omitempty"` // defaults to today when nil
	Currency string    `json:"currency,omitempty"`
}

// Basket defines a custom set of synthetic holdings to add to the portfolio.
type Basket struct {
	Mode             BasketMode   `json:"mode"`
	Items            []BasketItem `json:"items"`
	NotionalValue    float64      `json:"notional_value,omitempty"`    // required when Mode == "weight"
	NotionalCurrency string       `json:"notional_currency,omitempty"` // required when Mode == "weight"
	AcquiredAt       *DateOnly    `json:"acquired_at,omitempty"`       // defaults to today when nil
}

// BasketItem is one position in a custom basket.
type BasketItem struct {
	Symbol    string   `json:"symbol"`
	Exchange  string   `json:"exchange,omitempty"`
	Quantity  float64  `json:"quantity,omitempty"`   // used when Mode == "quantity"
	Weight    float64  `json:"weight,omitempty"`     // 0–1, used when Mode == "weight"
	CostBasis *float64 `json:"cost_basis,omitempty"` // overrides market price when set
	Currency  string   `json:"currency,omitempty"`   // defaults to NotionalCurrency for weight, "USD" for qty
}

// BacktestConfig drives a historical simulation of a basket.
type BacktestConfig struct {
	StartDate              DateOnly            `json:"start_date"`
	InitialAmount          float64             `json:"initial_amount"`
	Currency               string              `json:"currency"`
	Contribution           ContributionCadence `json:"contribution"`
	ContributionAmount     float64             `json:"contribution_amount,omitempty"`
	Rebalance              RebalanceMode       `json:"rebalance"`
	RebalanceThreshold     float64             `json:"rebalance_threshold,omitempty"` // drift % (e.g. 5 for 5%)
}

// Build constructs a synthetic *models.FlexQueryData from a ScenarioSpec.
// The returned value can be fed into all existing portfolio services unchanged.
// When spec.Backtest is set, spec.Basket must also be set; the returned data
// contains a complete synthetic trade stream from BacktestConfig.StartDate forward.
func Build(spec ScenarioSpec, realData *models.FlexQueryData, mp market.Provider, fxSvc *fx.Service) (*models.FlexQueryData, error) {
	if err := validateSpec(spec); err != nil {
		return nil, fmt.Errorf("invalid scenario spec: %w", err)
	}

	if spec.Backtest != nil {
		return buildBacktest(spec, mp, fxSvc)
	}

	// Start from the chosen base.
	var data *models.FlexQueryData
	switch spec.Base {
	case BaseModeRedirect:
		var err error
		data, err = buildRedirectScenario(spec, realData, mp, fxSvc)
		if err != nil {
			return nil, fmt.Errorf("building redirect scenario: %w", err)
		}
		// Redirect scenario already processes the basket dynamically based on cash flows.
		// We do not want to apply the basket again. Adjustments can still be applied if provided.
		// To prevent applyBasket from running, we can clear the Basket reference for the rest of the flow.
		spec.Basket = nil
	case BaseModeEmpty:
		data = &models.FlexQueryData{UserHash: realData.UserHash}
	default: // BaseModeReal
		data = deepCopyData(realData)
		if spec.BaseAsOf != nil {
			data = filterDataAsOf(data, spec.BaseAsOf.Time())
		}
	}

	// Basket adds synthetic buy trades on top of the base.
	if spec.Basket != nil {
		if err := applyBasket(data, spec.Basket, mp, fxSvc); err != nil {
			return nil, fmt.Errorf("applying basket: %w", err)
		}
	}

	// Adjustments add synthetic sell/buy trades on top of the (possibly basket-extended) base.
	// When BaseAsOf is set we pass it through so individual adjustments without an explicit
	// date default to the as-of date (and can fetch historical prices then).
	if len(spec.Adjustments) > 0 {
		var defaultDate *time.Time
		if spec.BaseAsOf != nil {
			d := spec.BaseAsOf.Time()
			defaultDate = &d
		}
		if err := applyAdjustments(data, spec.Adjustments, mp, defaultDate); err != nil {
			return nil, fmt.Errorf("applying adjustments: %w", err)
		}
	}

	return data, nil
}

// deepCopyData makes a shallow copy of FlexQueryData with independent trade and
// position slices so callers can safely append to them.
func deepCopyData(src *models.FlexQueryData) *models.FlexQueryData {
	out := &models.FlexQueryData{
		AccountID: src.AccountID,
		UserHash:  src.UserHash,
	}
	out.Trades = make([]models.Trade, len(src.Trades))
	copy(out.Trades, src.Trades)
	out.OpenPositions = make([]models.OpenPosition, len(src.OpenPositions))
	copy(out.OpenPositions, src.OpenPositions)
	out.CashTransactions = make([]models.CashTransaction, len(src.CashTransactions))
	copy(out.CashTransactions, src.CashTransactions)
	out.CashDividends = make([]models.CashDividend, len(src.CashDividends))
	copy(out.CashDividends, src.CashDividends)
	out.ParsedCorporateActions = make([]models.ParsedCorporateAction, len(src.ParsedCorporateActions))
	copy(out.ParsedCorporateActions, src.ParsedCorporateActions)
	return out
}

// filterDataAsOf returns a copy of data containing only trades and positions that
// existed on or before asOf. OpenPositions are always dropped because they reflect
// the current snapshot; the caller's downstream services will reconstruct from trades.
func filterDataAsOf(data *models.FlexQueryData, asOf time.Time) *models.FlexQueryData {
	endOfDay := time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 23, 59, 59, 0, time.UTC)
	out := &models.FlexQueryData{
		AccountID: data.AccountID,
		UserHash:  data.UserHash,
	}
	for _, t := range data.Trades {
		if !t.DateTime.After(endOfDay) {
			out.Trades = append(out.Trades, t)
		}
	}
	for _, ct := range data.CashTransactions {
		if !ct.DateTime.After(endOfDay) {
			out.CashTransactions = append(out.CashTransactions, ct)
		}
	}
	out.CashDividends = make([]models.CashDividend, len(data.CashDividends))
	copy(out.CashDividends, data.CashDividends)
	return out
}

// syntheticBuyTrade constructs a Trade representing a synthetic buy at the given price.
func syntheticBuyTrade(symbol, exchange, currency string, qty, price float64, dt time.Time) models.Trade {
	return models.Trade{
		Symbol:          symbol,
		ListingExchange: exchange,
		Currency:        currency,
		Quantity:        qty,
		Price:           price,
		Proceeds:        -(qty * price),
		DateTime:        dt,
		BuySell:         "BUY",
		EntryMethod:     "scenario",
	}
}

// syntheticSellTrade constructs a Trade representing a synthetic sell at the given price.
func syntheticSellTrade(symbol, exchange, currency string, qty, price float64, dt time.Time) models.Trade {
	return models.Trade{
		Symbol:          symbol,
		ListingExchange: exchange,
		Currency:        currency,
		Quantity:        -qty,
		Price:           price,
		Proceeds:        qty * price,
		DateTime:        dt,
		BuySell:         "SELL",
		EntryMethod:     "scenario",
	}
}

// findYahooSymbol returns the Yahoo Finance ticker for a symbol from existing trades.
// Falls back to symbol itself when no mapping is found.
func findYahooSymbol(trades []models.Trade, symbol, exchange string) string {
	for _, t := range trades {
		if t.Symbol == symbol && (exchange == "" || t.ListingExchange == exchange) && t.YahooSymbol != "" {
			return t.YahooSymbol
		}
	}
	return symbol
}

// findSymbolCurrency returns the native currency for a symbol from existing trades.
func findSymbolCurrency(trades []models.Trade, symbol, exchange string) string {
	for _, t := range trades {
		if t.Symbol == symbol && (exchange == "" || t.ListingExchange == exchange) {
			return t.Currency
		}
	}
	return ""
}

// symbolKey builds a composite key matching portfolio.posKey.
func symbolKey(symbol, exchange string) string {
	if exchange == "" {
		return symbol
	}
	return symbol + "@" + exchange
}

// today returns the current UTC date at midnight.
func today() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)
}
