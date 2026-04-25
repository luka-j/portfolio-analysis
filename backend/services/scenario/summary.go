package scenario

import (
	"fmt"
	"strings"
)

// RenderSummary returns a short human-readable rendering of the ScenarioSpec.
func RenderSummary(spec ScenarioSpec) string {
	var parts []string

	baseStr := "base=" + string(spec.Base)
	if spec.BaseAsOf != nil {
		baseStr += fmt.Sprintf(" as of %s", spec.BaseAsOf.Time().Format("2006-01-02"))
	}
	parts = append(parts, baseStr)

	if len(spec.Adjustments) > 0 {
		var adjs []string
		for _, a := range spec.Adjustments {
			dt := ""
			if a.Date != nil {
				dt = fmt.Sprintf(" on %s", a.Date.Time().Format("2006-01-02"))
			}
			switch a.Action {
			case ActionSellQty:
				adjs = append(adjs, fmt.Sprintf("sell %.4g shares of %s%s", a.Value, a.Symbol, dt))
			case ActionSellPct:
				adjs = append(adjs, fmt.Sprintf("sell %.4g%% of %s%s", a.Value, a.Symbol, dt))
			case ActionSellAll:
				adjs = append(adjs, fmt.Sprintf("sell all %s%s", a.Symbol, dt))
			case ActionBuy:
				adjs = append(adjs, fmt.Sprintf("buy %.4g %s of %s%s", a.Value, a.Currency, a.Symbol, dt))
			}
		}
		parts = append(parts, "adjustments ("+strings.Join(adjs, ", ")+")")
	}

	if spec.Basket != nil {
		var items []string
		for _, v := range spec.Basket.Items {
			if spec.Basket.Mode == BasketModeWeight {
				items = append(items, fmt.Sprintf("%s at %.1f%%", v.Symbol, v.Weight*100))
			} else {
				items = append(items, fmt.Sprintf("%s %.4g shares", v.Symbol, v.Quantity))
			}
		}
		parts = append(parts, "basket ("+strings.Join(items, ", ")+")")
	}

	if spec.Backtest != nil {
		parts = append(parts, fmt.Sprintf("backtest (start %s)", spec.Backtest.StartDate.Time().Format("2006-01-02")))
	}

	return strings.Join(parts, "; ")
}
