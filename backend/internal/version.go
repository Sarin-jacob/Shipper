package internal

import (
	"fmt"
	"strconv"
	"strings"
)

// IncrementPatch takes a version like "0.1.0" and returns "0.1.1"
func IncrementPatch(currentVersion string) (string, error) {
	if currentVersion == "" {
		return "0.1.0", nil // Initial version
	}

	parts := strings.Split(currentVersion, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid version format, expected x.y.z")
	}

	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", fmt.Errorf("invalid patch version: %v", err)
	}

	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1), nil
}

// GenerateTags creates the array of tags to push (version, commit, latest, plus custom)
func GenerateTags(imageName, version, commitHash string, customTags []string) []string {
	tags := []string{
		fmt.Sprintf("%s:%s", imageName, version),
		fmt.Sprintf("%s:commit-%s", imageName, commitHash[:7]), // short commit
		fmt.Sprintf("%s:latest", imageName),
	}

	// Add any custom tags (e.g., stable, prod)
	for _, ct := range customTags {
		tags = append(tags, fmt.Sprintf("%s:%s", imageName, ct))
	}

	return tags
}