// backend/internal/config.go
package internal

import (
	"os"
	"time"
)

// Config holds all application settings
type Config struct {
	Port         string
	DBPath       string
	RegistryURL  string
	PollInterval time.Duration
	DataDir      string
	StaticDir    string
	RegistryContainer string
}

// LoadConfig parses environment variables and returns a populated Config
func LoadConfig() Config {
	return Config{
		Port:         getEnv("SHIPPER_PORT", "8080"),
		DBPath:       getEnv("SHIPPER_DB_PATH", "./data/shiper.db"),
		RegistryURL:  getEnv("SHIPPER_REGISTRY", "localhost:8000"),
		PollInterval: getEnvDuration("SHIPPER_POLL_INTERVAL", 1*time.Hour),
		DataDir:      getEnv("SHIPPER_DATA_DIR", "./data"),
		StaticDir:    getEnv("SHIPPER_STATIC_DIR", "./static"),
		RegistryContainer: getEnv("SHIPPER_REGISTRY_CONTAINER", "shipper_registry"),
	}
}

// getEnv fetches a string from env or returns the fallback
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// getEnvDuration parses a duration from env or returns the fallback
func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		d, err := time.ParseDuration(value)
		if err == nil {
			return d
		}
	}
	return fallback
}