// backend/internal/git.go
package internal

import (
	"fmt"
	"os/exec"
)

// Clone repo to a temporary directory
func Clone(repoURL, branch, destPath string) error {
	// --depth 1 makes the clone incredibly fast since we only need the latest files to build
	cmd := exec.Command("git", "clone", "--depth", "1", "--branch", branch, repoURL, destPath)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %v\nOutput: %s", err, string(output))
	}
	
	return nil
}