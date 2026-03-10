// backend/internal/git.go
package internal

import (
	"fmt"
	"os/exec"
	"strings"
)

// CloneRepo performs a shallow clone of the target branch into destPath
func CloneRepo(repoURL, branch, destPath string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", "--branch", branch, repoURL, destPath)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %v\nOutput: %s", err, string(output))
	}
	
	return nil
}

// GetLocalCommitHash gets the HEAD commit hash from a local directory
func GetLocalCommitHash(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get local commit hash: %v", err)
	}
	
	return strings.TrimSpace(string(out)), nil
}

// GetRemoteCommitHash fetches the latest commit hash from the remote repository without cloning
func GetRemoteCommitHash(repoURL, branch string) (string, error) {
	cmd := exec.Command("git", "ls-remote", repoURL, branch)
	
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get remote commit hash: %v", err)
	}

	// Output format is: "<hash>\trefs/heads/<branch>\n"
	fields := strings.Fields(string(out))
	if len(fields) > 0 {
		return fields[0], nil
	}

	return "", fmt.Errorf("could not parse remote commit hash")
}