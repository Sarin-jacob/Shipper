// backend/internal/settings.go
package internal

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type GlobalSettings struct {
	PollInterval    string   `yaml:"poll_interval"`    // e.g. "30m"
	RetentionPolicy string   `yaml:"retention_policy"` // "all" or "one_per_minor"
	GHToken         string   `yaml:"gh_token"`         // Hidden from UI
	Registries      []string `yaml:"registries"`       // List of available registries
}

// LoadSettings reads the YAML file from the data directory
func LoadSettings(dataDir string) GlobalSettings {
	path := filepath.Join(dataDir, "shipper.yml")
	
	// Default settings
	settings := GlobalSettings{
		PollInterval:    "1h",
		RetentionPolicy: "one_per_minor",
		Registries:      []string{},
	}

	data, err := os.ReadFile(path)
	if err == nil {
		yaml.Unmarshal(data, &settings)
	} else {
		// Create the file if it doesn't exist
		SaveSettings(dataDir, settings)
	}

	return settings
}

// SaveSettings writes the struct back to YAML
func SaveSettings(dataDir string, settings GlobalSettings) error {
	path := filepath.Join(dataDir, "shipper.yml")
	data, err := yaml.Marshal(&settings)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600) // 0600 ensures only the shipper user can read the token
}

// InjectGHToken safely adds the token to a GitHub URL for cloning
func InjectGHToken(repoURL, token string) string {
	if token != "" && strings.Contains(repoURL, "github.com") && !strings.Contains(repoURL, "@") {
		return strings.Replace(repoURL, "https://github.com/", "https://"+token+"@github.com/", 1)
	}
	return repoURL
}