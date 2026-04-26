package scenario

import (
	"fmt"
	"math"
)

// validateSpec returns an error if the ScenarioSpec is structurally invalid.
func validateSpec(spec ScenarioSpec) error {
	if spec.Base != BaseModeReal && spec.Base != BaseModeEmpty && spec.Base != BaseModeRedirect {
		return fmt.Errorf("base must be %q, %q or %q, got %q", BaseModeReal, BaseModeEmpty, BaseModeRedirect, spec.Base)
	}
	if spec.Base == BaseModeRedirect {
		if spec.Basket == nil {
			return fmt.Errorf("redirect base requires a target basket")
		}
		if err := validateBasket(spec.Basket, false); err != nil {
			return fmt.Errorf("redirect basket: %w", err)
		}
		return nil // adjustments are ignored for redirect
	}
	if spec.Backtest != nil {
		if spec.Basket == nil {
			return fmt.Errorf("backtest requires a basket to define target allocation")
		}
		if spec.Backtest.InitialAmount <= 0 {
			return fmt.Errorf("backtest initial_amount must be positive")
		}
		if spec.Backtest.Currency == "" {
			return fmt.Errorf("backtest currency is required")
		}
		if err := validateBasket(spec.Basket, false); err != nil {
			return fmt.Errorf("backtest basket: %w", err)
		}
		return nil // adjustments are ignored for backtests
	}
	if spec.Basket != nil {
		if err := validateBasket(spec.Basket, true); err != nil {
			return err
		}
	}
	for i, adj := range spec.Adjustments {
		if err := validateAdjustment(adj, i); err != nil {
			return err
		}
	}
	return nil
}

func validateBasket(b *Basket, requireNotional bool) error {
	if len(b.Items) == 0 {
		return fmt.Errorf("basket must have at least one item")
	}
	if b.Mode != BasketModeQuantity && b.Mode != BasketModeWeight {
		return fmt.Errorf("basket mode must be %q or %q", BasketModeQuantity, BasketModeWeight)
	}
	if b.Mode == BasketModeWeight {
		if requireNotional && b.NotionalValue <= 0 {
			return fmt.Errorf("basket notional_value must be positive in weight mode")
		}
		if requireNotional && b.NotionalCurrency == "" {
			return fmt.Errorf("basket notional_currency is required in weight mode")
		}
		var sum float64
		for _, item := range b.Items {
			if item.Weight < 0 {
				return fmt.Errorf("basket item %q has negative weight", item.Symbol)
			}
			sum += item.Weight
		}
		if math.Abs(sum-1.0) > 0.001 {
			return fmt.Errorf("basket weights sum to %.4f, must be within 0.001 of 1.0", sum)
		}
	}
	for _, item := range b.Items {
		if item.Symbol == "" {
			return fmt.Errorf("basket item is missing symbol")
		}
		if b.Mode == BasketModeQuantity && item.Quantity <= 0 {
			return fmt.Errorf("basket item %q quantity must be positive in quantity mode", item.Symbol)
		}
	}
	return nil
}

func validateAdjustment(adj Adjustment, idx int) error {
	if adj.Symbol == "" {
		return fmt.Errorf("adjustment[%d]: symbol is required", idx)
	}
	switch adj.Action {
	case ActionSellQty:
		if adj.Value <= 0 {
			return fmt.Errorf("adjustment[%d]: sell_qty value must be positive", idx)
		}
	case ActionSellPct:
		if adj.Value <= 0 || adj.Value > 100 {
			return fmt.Errorf("adjustment[%d]: sell_pct value must be between 0 and 100", idx)
		}
	case ActionSellAll:
		// no value required
	case ActionBuy:
		if adj.Value <= 0 {
			return fmt.Errorf("adjustment[%d]: buy value must be positive", idx)
		}
	default:
		return fmt.Errorf("adjustment[%d]: unknown action %q", idx, adj.Action)
	}
	return nil
}
