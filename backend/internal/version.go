// backend/internal/version.go
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

// GenerateTags creates the array of tags to push
func GenerateTags(imageName, version, commitHash string, customTags []string) []string {
	// Shorten commit hash to 7 characters
	shortCommit := commitHash
	if len(commitHash) > 7 {
		shortCommit = commitHash[:7]
	}

	tags := []string{
		fmt.Sprintf("%s:%s", imageName, version),
		fmt.Sprintf("%s:commit-%s", imageName, shortCommit),
		fmt.Sprintf("%s:latest", imageName),
	}

	for _, ct := range customTags {
		tags = append(tags, fmt.Sprintf("%s:%s", imageName, ct))
	}

	return tags
}

// BumpMinor: "0.1.2" -> "0.2.0"
func BumpMinor(currentVersion string) (string, error) {
	if currentVersion == "" {
		return "0.1.0", nil
	}
	parts := strings.Split(currentVersion, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid version format")
	}
	minor, _ := strconv.Atoi(parts[1])
	return fmt.Sprintf("%s.%d.0", parts[0], minor+1), nil
}

// BumpMajor: "0.1.2" -> "1.0.0"
func BumpMajor(currentVersion string) (string, error) {
	if currentVersion == "" {
		return "1.0.0", nil
	}
	parts := strings.Split(currentVersion, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid version format")
	}
	major, _ := strconv.Atoi(parts[0])
	return fmt.Sprintf("%d.0.0", major+1), nil
}