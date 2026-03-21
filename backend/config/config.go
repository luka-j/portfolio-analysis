package config

import (
	"os"
	"strings"
)

// Config holds all application configuration.
type Config struct {
	Port               string
	DataDir            string
	DatabaseURL        string
	AllowedTokenHashes []string // SHA-256 hashes of allowed tokens; empty = open mode
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	cfg := &Config{
		Port:        getEnv("PORT", "8080"),
		DataDir:     getEnv("DATA_DIR", "./data"),
		DatabaseURL: getEnv("DATABASE_URL", "host=localhost user=postgres password=postgres dbname=gofolio port=5432 sslmode=disable"),
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
