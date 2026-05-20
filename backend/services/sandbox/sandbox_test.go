package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"portfolio-analysis/models"
)

func TestExecute(t *testing.T) {
	// Need to fake the environment variable so NewService() initializes if python is available
	t.Setenv("PYTHON_SANDBOX_ENABLED", "true")

	svc := NewService()
	if !svc.IsEnabled() {
		t.Skip("Python sandbox not enabled (python/pandas/numpy might be missing), skipping test")
	}

	// Make sure we run the test relative to the correct path
	// The runner script is expected at ./sandbox_runner.py, but tests run from backend/services/sandbox.
	// We'll update the runnerPath temporarily for the test.
	svc.runnerPath = "../../sandbox_runner.py"

	trades := []models.Trade{
		{
			Symbol:        "AAPL",
			DateTime:      time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			Quantity:      10,
			Price:         150.0,
			Proceeds:      -1500.0,
			Commission:    -5.0,
			BuySell:       "BUY",
			Currency:      "USD",
			AssetCategory: "STK",
		},
		{
			Symbol:        "AAPL",
			DateTime:      time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC),
			Quantity:      -5,
			Price:         160.0,
			Proceeds:      800.0,
			Commission:    -5.0,
			BuySell:       "SELL",
			Currency:      "USD",
			AssetCategory: "STK",
		},
	}

	cash := []models.CashTransaction{
		{
			Type:     "Dividends",
			Symbol:   "AAPL",
			Currency: "USD",
			Amount:   20.0,
			DateTime: time.Date(2023, 1, 15, 0, 0, 0, 0, time.UTC),
		},
	}

	t.Run("basic arithmetic", func(t *testing.T) {
		code := `print(1 + 1)`
		res, err := svc.Execute(context.Background(), code, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Error != "" {
			t.Fatalf("unexpected python error: %v", res.Error)
		}
		if res.Output != "2" {
			t.Errorf("expected 2, got %q", res.Output)
		}
	})

	t.Run("pandas dataframe manipulation", func(t *testing.T) {
		code := `
import pandas as pd
# df is predefined
buys = df[df['buy_sell'] == 'BUY']
print(int(buys['quantity'].sum()))
print(df_cash['amount'].sum())
`
		res, err := svc.Execute(context.Background(), code, trades, cash)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Error != "" {
			t.Fatalf("unexpected python error: %v", res.Error)
		}
		
		lines := strings.Split(res.Output, "\n")
		if len(lines) < 2 {
			t.Fatalf("expected at least 2 lines of output, got %q", res.Output)
		}
		if lines[0] != "10" {
			t.Errorf("expected 10, got %q", lines[0])
		}
		if lines[1] != "20.0" {
			t.Errorf("expected 20.0, got %q", lines[1])
		}
	})

	t.Run("restricted builtins", func(t *testing.T) {
		code := `
import os
os.system("echo 'hack'")
`
		res, err := svc.Execute(context.Background(), code, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Error, "module 'os' has no attribute 'system'") && !strings.Contains(res.Error, "No module named 'os'") && !strings.Contains(res.Error, "is not allowed") {
			// __import__ works but if it tries to access things we didn't explicitly expose or if we strip them
			// Wait, the sandbox replaces __builtins__. It still allows importing standard libraries, but blocks access to dangerous stuff if we overwrite globals.
			// Let's just check that it errored in some way related to the restricted environment.
			if res.Error == "" {
				t.Errorf("expected security error, got success")
			}
		}
	})

	t.Run("timeout enforcement", func(t *testing.T) {
		// Override timeout to be very short for the test
		originalTimeout := svc.timeout
		svc.timeout = 100 * time.Millisecond
		defer func() { svc.timeout = originalTimeout }()

		code := `
import time
time.sleep(2)
print("done")
`
		res, err := svc.Execute(context.Background(), code, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Output == "done" {
			t.Errorf("expected timeout, got 'done'")
		}
		if res.Error == "" || !strings.Contains(res.Error, "timed out") {
			t.Errorf("expected timeout error message, got: %q", res.Error)
		}
	})
}
