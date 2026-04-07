package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all application configuration.
type Config struct {
	Port               string
	MetricsPort        string // METRICS_PORT, default "9090"
	DataDir            string
	DatabaseURL        string
	AllowedTokenHashes []string // SHA-256 hashes of allowed tokens; empty = open mode
	CORSOrigin         string   // CORS_ORIGIN, default "http://localhost:5173"

	// Provider ordering (comma-separated names, first = highest priority).
	FundamentalsProviders string // FUNDAMENTALS_PROVIDERS, default "Yahoo"
	BreakdownProviders    string // BREAKDOWN_PROVIDERS,    default "Yahoo"

	GeminiAPIKey     string // GEMINI_API_KEY
	GeminiFlashModel string // GEMINI_FLASH_MODEL, default "gemini-3.1-flash-lite-preview"
	GeminiProModel   string // GEMINI_PRO_MODEL,   default "gemini-3.1-pro-preview"

	// CashBucketExpiryDays is the number of days sale proceeds are held in a
	// temporary bucket before being counted as a real portfolio outflow. Set to
	// 0 to disable the feature and revert to the legacy behaviour.
	CashBucketExpiryDays int // CASH_BUCKET_EXPIRY_DAYS, default 30
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	cfg := &Config{
		Port:        getEnv("PORT", "8080"),
		MetricsPort: getEnv("METRICS_PORT", "9090"),
		DataDir:     getEnv("DATA_DIR", "./data"),
		DatabaseURL: getEnv("DATABASE_URL", "host=localhost user=postgres password=postgres dbname=portfolio port=5432 sslmode=disable"),
		CORSOrigin:  getEnv("CORS_ORIGIN", "http://localhost:5173"),

		FundamentalsProviders: getEnv("FUNDAMENTALS_PROVIDERS", "Yahoo"),
		BreakdownProviders:    getEnv("BREAKDOWN_PROVIDERS", "Yahoo"),

		GeminiAPIKey:     getEnv("GEMINI_API_KEY", ""),
		GeminiFlashModel: getEnv("GEMINI_FLASH_MODEL", "gemini-3.1-flash-lite-preview"),
		GeminiProModel:   getEnv("GEMINI_PRO_MODEL", "gemini-3.1-pro-preview"),

		CashBucketExpiryDays: getEnvInt("CASH_BUCKET_EXPIRY_DAYS", 30),
	}

	if v := os.Getenv("ALLOWED_TOKEN_HASHES"); v != "" {
		cfg.AllowedTokenHashes = strings.Split(v, ",")
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
