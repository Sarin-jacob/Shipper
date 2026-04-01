// backend/internal/git.go
package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CloneRepo performs a shallow clone of the target branch into destPath
func CloneRepo(repoURL, branch, destPath, ghToken, dataDir string) error {
	// Inject the token safely
	repoURL = InjectGHToken(repoURL, ghToken)

	cmd := exec.Command("git", "clone", "--depth", "1", "--branch", branch, repoURL, destPath)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %v\nOutput: %s", err, string(output))
	}
	
	lfsCacheDir := filepath.Join(dataDir, "lfs_cache")
	os.MkdirAll(lfsCacheDir, os.ModePerm)

	configCmd := exec.Command("git", "config", "lfs.storage", lfsCacheDir)
	configCmd.Dir = destPath // Execute inside the cloned repo
	if output, err := configCmd.CombinedOutput(); err != nil {
		fmt.Printf("Warning: failed to set LFS storage path: %v\nOutput: %s", err, string(output))
	}

	lfsCmd := exec.Command("git", "lfs", "pull")
	lfsCmd.Dir = destPath
	lfsCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := lfsCmd.CombinedOutput(); err != nil {
		fmt.Printf("Warning: 'git lfs pull' failed: %v\nOutput: %s", err, string(output))
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
func GetRemoteCommitHash(repoURL, branch, ghToken string) (string, error) {
	repoURL = InjectGHToken(repoURL, ghToken)

	cmd := exec.Command("git", "ls-remote", repoURL, branch)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get remote commit hash: %v", err)
	}

	fields := strings.Fields(string(out))
	if len(fields) > 0 {
		return fields[0], nil
	}

	return "", fmt.Errorf("could not parse remote commit hash")
}