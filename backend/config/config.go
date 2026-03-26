package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all application configuration.
type Config struct {
	Port               string
	DataDir            string
	DatabaseURL        string
	AllowedTokenHashes []string // SHA-256 hashes of allowed tokens; empty = open mode
	CORSOrigin         string   // CORS_ORIGIN, default "http://localhost:5173"

	// External API keys for fundamentals data.
	FMPAPIKey string // FMP_API_KEY

	// Rate limits per provider (default = free-tier limits).
	FMPRequestsPerMinute int // FMP_REQUESTS_PER_MINUTE, default 10
	FMPRequestsPerDay    int // FMP_REQUESTS_PER_DAY,    default 250

	// Provider ordering (comma-separated names, first = highest priority).
	FundamentalsProviders string // FUNDAMENTALS_PROVIDERS, default "FMP"
	BreakdownProviders    string // BREAKDOWN_PROVIDERS,    default "Yahoo"
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	cfg := &Config{
		Port:        getEnv("PORT", "8080"),
		DataDir:     getEnv("DATA_DIR", "./data"),
		DatabaseURL: getEnv("DATABASE_URL", "host=localhost user=postgres password=postgres dbname=gofolio port=5432 sslmode=disable"),
		CORSOrigin:  getEnv("CORS_ORIGIN", "http://localhost:5173"),

		FMPAPIKey: getEnv("FMP_API_KEY", ""),

		FMPRequestsPerMinute: getEnvInt("FMP_REQUESTS_PER_MINUTE", 10),
		FMPRequestsPerDay:    getEnvInt("FMP_REQUESTS_PER_DAY", 250),

		FundamentalsProviders: getEnv("FUNDAMENTALS_PROVIDERS", "FMP"),
		BreakdownProviders:    getEnv("BREAKDOWN_PROVIDERS", "Yahoo"),
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
