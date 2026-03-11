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
}

// LoadConfig parses environment variables and returns a populated Config
func LoadConfig() Config {
	return Config{
		Port:         getEnv("SHIPER_PORT", "8080"),
		DBPath:       getEnv("SHIPER_DB_PATH", "./data/shiper.db"),
		RegistryURL:  getEnv("SHIPER_REGISTRY", "oci.jell0.online"),
		PollInterval: getEnvDuration("SHIPER_POLL_INTERVAL", 1*time.Hour),
		DataDir:      getEnv("SHIPER_DATA_DIR", "./data"),
		StaticDir:    getEnv("SHIPER_STATIC_DIR", "./static"),
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