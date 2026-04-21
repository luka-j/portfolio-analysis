package stats

import (
	"math"
	"testing"
)

func TestCumulative_Empty(t *testing.T) {
	if got := CalculateCumulativeReturnSeries(nil, nil, ""); len(got) != 0 {
		t.Fatalf("expected empty on nil input, got %+v", got)
	}
	// Length mismatch → nil.
	if got := CalculateCumulativeReturnSeries([]float64{0.01}, []string{"2024-01-01", "2024-01-02"}, ""); got != nil {
		t.Fatalf("expected nil on length mismatch, got %+v", got)
	}
}

func TestCumulative_FirstDatePrepended(t *testing.T) {
	returns := []float64{0.01, -0.02, 0.03}
	dates := []string{"2024-01-02", "2024-01-03", "2024-01-04"}
	out := CalculateCumulativeReturnSeries(returns, dates, "2024-01-01")
	if len(out) != 4 {
		t.Fatalf("expected 4 points (first date + 3 returns), got %d", len(out))
	}
	if out[0].Date != "2024-01-01" || out[0].Value != 0 {
		t.Fatalf("first point must be baseline 0 on firstDate, got %+v", out[0])
	}
	// Chained compounding: (1.01)(0.98)(1.03) - 1, in percent.
	want := (1.01*0.98*1.03 - 1) * 100
	if math.Abs(out[3].Value-want) > 1e-9 {
		t.Fatalf("last cumulative: want %.9f, got %.9f", want, out[3].Value)
	}
	if out[3].Date != "2024-01-04" {
		t.Fatalf("last date mismatch: %s", out[3].Date)
	}
}

func TestCumulative_NoFirstDate(t *testing.T) {
	returns := []float64{0.10, 0.10}
	dates := []string{"2024-01-02", "2024-01-03"}
	out := CalculateCumulativeReturnSeries(returns, dates, "")
	if len(out) != 2 {
		t.Fatalf("expected 2 points, got %d", len(out))
	}
	if math.Abs(out[0].Value-10) > 1e-9 {
		t.Fatalf("point 0 should be 10%%, got %.6f", out[0].Value)
	}
	if math.Abs(out[1].Value-21) > 1e-9 {
		t.Fatalf("point 1 should be 21%% (1.1*1.1=1.21), got %.6f", out[1].Value)
	}
}

func TestCumulative_FullDrawdown(t *testing.T) {
	// 100% loss → cumulative is -100%. Subsequent returns can't recover (growth stays 0).
	returns := []float64{-1.0, 0.5, 0.5}
	dates := []string{"a", "b", "c"}
	out := CalculateCumulativeReturnSeries(returns, dates, "")
	if len(out) != 3 {
		t.Fatal("wrong length")
	}
	for _, p := range out {
		if math.Abs(p.Value-(-100)) > 1e-9 {
			t.Fatalf("after wipeout all points should be -100%%, got %+v", p)
		}
	}
}
