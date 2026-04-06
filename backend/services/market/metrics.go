package market

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// yahooAPIRequests counts every outbound Yahoo Finance HTTP request by endpoint, call type, and HTTP status.
	yahooAPIRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "yahoo_api_requests_total",
		Help: "Total number of outbound Yahoo Finance API requests.",
	}, []string{"endpoint", "request_type", "status_code"})

	// yahooAPIRequestDuration records time-to-first-byte latency of each outbound Yahoo Finance HTTP request.
	yahooAPIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "yahoo_api_request_duration_seconds",
		Help:    "Time-to-first-byte latency of outbound Yahoo Finance API requests in seconds.",
		Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30},
	}, []string{"endpoint", "request_type"})
)

// observeYahooRequest records one completed outbound Yahoo Finance HTTP request.
//
// endpoint is the API group: "chart" (v8 chart API) or "quoteSummary" (v10 quoteSummary API).
// requestType is the logical call context: "history", "quote_type", "currency",
// "current_price", "etf_breakdown", or "asset_profile".
// status is the HTTP response status code; pass 0 for transport-level errors.
func observeYahooRequest(endpoint, requestType string, status int, elapsed time.Duration) {
	sc := strconv.Itoa(status)
	if status == 0 {
		sc = "error"
	}
	yahooAPIRequests.WithLabelValues(endpoint, requestType, sc).Inc()
	yahooAPIRequestDuration.WithLabelValues(endpoint, requestType).Observe(elapsed.Seconds())
}
