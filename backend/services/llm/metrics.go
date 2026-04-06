package llm

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// geminiRequests counts every GenerateContent call by model, call type, and outcome.
	geminiRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gemini_api_requests_total",
		Help: "Total number of Gemini API GenerateContent calls.",
	}, []string{"model", "call_type", "outcome"})

	// geminiRequestDuration records end-to-end latency of each GenerateContent call.
	geminiRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gemini_api_request_duration_seconds",
		Help:    "End-to-end latency of Gemini API GenerateContent calls in seconds.",
		Buckets: []float64{1, 2.5, 5, 10, 20, 30, 60, 90, 120},
	}, []string{"model", "call_type"})

	// geminiInputTokens accumulates prompt tokens sent per model and call type.
	geminiInputTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gemini_api_input_tokens_total",
		Help: "Total prompt tokens sent to the Gemini API.",
	}, []string{"model", "call_type"})

	// geminiOutputTokens accumulates candidate tokens received per model and call type.
	geminiOutputTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gemini_api_output_tokens_total",
		Help: "Total candidate tokens received from the Gemini API.",
	}, []string{"model", "call_type"})
)
