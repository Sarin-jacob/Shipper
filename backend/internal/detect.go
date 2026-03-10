// backend/internal/detect.go
package internal

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// BuildTarget represents what the UI will ask the user to confirm
type BuildTarget struct {
	Type        string `json:"type"` // "compose" or "dockerfile"
	File        string `json:"file"`
	ServiceName string `json:"service_name,omitempty"`
	Context     string `json:"context"`
	Dockerfile  string `json:"dockerfile"`
}

// ComposeSchema is a minimal struct to extract just what we need
type ComposeSchema struct {
	Services map[string]struct {
		Build yaml.Node `yaml:"build"`
	} `yaml:"services"`
}

func AnalyzeRepo(repoPath string) (*BuildTarget, error) {
	composeFiles := []string{"docker-compose.yml", "compose.yml", "docker-compose.yaml"}

	// 1. Check for Compose files first
	for _, file := range composeFiles {
		fullPath := filepath.Join(repoPath, file)
		if _, err := os.Stat(fullPath); err == nil {
			return parseCompose(fullPath, file)
		}
	}

	// 2. Fallback to standalone Dockerfile
	dockerfilePath := filepath.Join(repoPath, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err == nil {
		return &BuildTarget{
			Type:       "dockerfile",
			File:       "Dockerfile",
			Context:    ".",
			Dockerfile: "Dockerfile",
		}, nil
	}

	return nil, fmt.Errorf("no valid build configuration found (checked compose and Dockerfile)")
}

func parseCompose(fullPath, fileName string) (*BuildTarget, error) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}

	var compose ComposeSchema
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %v", err)
	}

	// Find the first service that has a "build" directive
	for serviceName, service := range compose.Services {
		if service.Build.IsZero() {
			continue
		}

		target := &BuildTarget{
			Type:        "compose",
			File:        fileName,
			ServiceName: serviceName,
			Context:     ".",
			Dockerfile:  "Dockerfile",
		}

		// Handle string build context: `build: .`
		if service.Build.Kind == yaml.ScalarNode {
			target.Context = service.Build.Value
			return target, nil
		}

		// Handle object build context: 
		// build:
		//   context: .
		//   dockerfile: custom.Dockerfile
		if service.Build.Kind == yaml.MappingNode {
			var buildObj struct {
				Context    string `yaml:"context"`
				Dockerfile string `yaml:"dockerfile"`
			}
			service.Build.Decode(&buildObj)

			if buildObj.Context != "" {
				target.Context = buildObj.Context
			}
			if buildObj.Dockerfile != "" {
				target.Dockerfile = buildObj.Dockerfile
			}
			return target, nil
		}
	}

	return nil, fmt.Errorf("found compose file but no build directives inside")
}