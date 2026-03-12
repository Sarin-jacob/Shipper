// backend/internal/settings.go
package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type RegistryAuth struct {
	URL      string `yaml:"url" json:"url"`
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
}

type GlobalSettings struct {
	PollInterval    string         `yaml:"poll_interval"`
	RetentionPolicy string         `yaml:"retention_policy"`
	GHToken         string         `yaml:"gh_token"`
	Registries      []RegistryAuth `yaml:"registries"` 
}

// LoadSettings reads the YAML file from the data directory
func LoadSettings(dataDir string) GlobalSettings {
	path := filepath.Join(dataDir, "shipper.yml")
	
	// Default settings
	settings := GlobalSettings{
		PollInterval:    "1h",
		RetentionPolicy: "one_per_minor",
		Registries:      []RegistryAuth{},
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
	return os.WriteFile(path, data, 0644) // 0600 ensures only the shipper user can read the token
}

// InjectGHToken safely adds the token to a GitHub URL for cloning
func InjectGHToken(repoURL, token string) string {
	if token != "" && strings.Contains(repoURL, "github.com") && !strings.Contains(repoURL, "@") {
		return strings.Replace(repoURL, "https://github.com/", "https://"+token+"@github.com/", 1)
	}
	return repoURL
}

func DockerLogin(registry RegistryAuth) error {
	if registry.Username == "" || registry.Password == "" {
		return nil // No auth provided, skip login
	}
	cmd := exec.Command("docker", "login", registry.URL, "-u", registry.Username, "--password-stdin")
	cmd.Stdin = strings.NewReader(registry.Password)
	return cmd.Run()
}