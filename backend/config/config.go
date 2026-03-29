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

	// Provider ordering (comma-separated names, first = highest priority).
	FundamentalsProviders string // FUNDAMENTALS_PROVIDERS, default "Yahoo"
	BreakdownProviders    string // BREAKDOWN_PROVIDERS,    default "Yahoo"

	GeminiAPIKey      string // GEMINI_API_KEY
	GeminiSummaryModel string // GEMINI_SUMMARY_MODEL, default "gemini-3.1-flash-lite-preview"
	GeminiChatModel    string // GEMINI_CHAT_MODEL,    default "gemini-3.1-pro-preview"
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	cfg := &Config{
		Port:        getEnv("PORT", "8080"),
		DataDir:     getEnv("DATA_DIR", "./data"),
		DatabaseURL: getEnv("DATABASE_URL", "host=localhost user=postgres password=postgres dbname=gofolio port=5432 sslmode=disable"),
		CORSOrigin:  getEnv("CORS_ORIGIN", "http://localhost:5173"),

		FundamentalsProviders: getEnv("FUNDAMENTALS_PROVIDERS", "Yahoo"),
		BreakdownProviders:    getEnv("BREAKDOWN_PROVIDERS", "Yahoo"),

		GeminiAPIKey:       getEnv("GEMINI_API_KEY", ""),
		GeminiSummaryModel: getEnv("GEMINI_SUMMARY_MODEL", "gemini-3.1-flash-lite-preview"),
		GeminiChatModel:    getEnv("GEMINI_CHAT_MODEL", "gemini-3.1-pro-preview"),
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
