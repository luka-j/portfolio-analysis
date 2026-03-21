package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gofolio-analysis/models"
)

// TestExchangeAwarePositions verifies that the same symbol traded on two different
// exchanges (VUAA@LSE and VUAA@XMIL) produces two independent portfolio positions.
func TestExchangeAwarePositions(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	// The sample XML contains VUAA traded on two exchanges:
	// VUAA@LSE (USD, 50 shares) and VUAA@XMIL (EUR, 30 shares).
	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts, "/api/v1/portfolio/value?currencies=USD", testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.PortfolioValueResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	// Collect VUAA positions.
	var vuaaPositions []models.PositionValue
	for _, pos := range result.Positions {
		if pos.Symbol == "VUAA" {
			vuaaPositions = append(vuaaPositions, pos)
		}
	}

	// Must be two distinct positions — one for each exchange.
	require.Len(t, vuaaPositions, 2, "VUAA should appear as two separate positions (LSE and XMIL)")

	// Collect exchanges and verify both are present.
	exchanges := make(map[string]bool)
	for _, pos := range vuaaPositions {
		exchanges[pos.ListingExchange] = true
	}
	assert.True(t, exchanges["LSE"], "VUAA@LSE position should be present")
	assert.True(t, exchanges["XMIL"], "VUAA@XMIL position should be present")

	// Quantities must be correct and independent.
	for _, pos := range vuaaPositions {
		switch pos.ListingExchange {
		case "LSE":
			assert.Equal(t, 50.0, pos.Quantity, "VUAA@LSE quantity should be 50")
			assert.Equal(t, "USD", pos.NativeCurrency)
		case "XMIL":
			assert.Equal(t, 30.0, pos.Quantity, "VUAA@XMIL quantity should be 30")
			assert.Equal(t, "EUR", pos.NativeCurrency)
		}
	}
}

// TestExchangeAwareTradesFilter verifies that the ?exchange= query parameter
// properly scopes trade history to a single listing.
func TestExchangeAwareTradesFilter(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	t.Run("VUAA@LSE trades only", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/trades?symbol=VUAA&exchange=LSE&currency=USD", testToken)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

		trades, _ := result["trades"].([]interface{})
		require.Len(t, trades, 1, "VUAA@LSE should have exactly 1 trade")
		first := trades[0].(map[string]interface{})
		assert.Equal(t, "BUY", first["side"])
		assert.Equal(t, 50.0, first["quantity"])
		assert.Equal(t, 80.0, first["price"])
	})

	t.Run("VUAA@XMIL trades only", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/trades?symbol=VUAA&exchange=XMIL&currency=EUR", testToken)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

		trades, _ := result["trades"].([]interface{})
		require.Len(t, trades, 1, "VUAA@XMIL should have exactly 1 trade")
		first := trades[0].(map[string]interface{})
		assert.Equal(t, "BUY", first["side"])
		assert.Equal(t, 30.0, first["quantity"])
		assert.Equal(t, 75.0, first["price"])
	})

	t.Run("VUAA all exchanges returns both trades", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/trades?symbol=VUAA&currency=USD", testToken)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

		trades, _ := result["trades"].([]interface{})
		require.Len(t, trades, 2, "VUAA without exchange filter should return trades from both exchanges")
	})
}
