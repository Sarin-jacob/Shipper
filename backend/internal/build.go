package internal

import (
	"fmt"
	"os/exec"
)

// RunBuildx executes the Docker buildx command in the cloned directory
func RunBuildx(workDir string, dockerfile string, context string, tags []string, push bool) (string, error) {
	args := []string{"buildx", "build", "-f", dockerfile}

	// Append all tags
	for _, tag := range tags {
		args = append(args, "-t", tag)
	}

	// Add push flag if we are pushing to oci.jell0.online
	if push {
		args = append(args, "--push")
	}

	// Append the build context (usually ".")
	args = append(args, context)

	cmd := exec.Command("docker", args...)
	cmd.Dir = workDir // Execute inside the cloned repo

	// In a real scenario, we'd pipe this to a file or websocket for real-time logs (Section 10).
	// For now, we capture CombinedOutput.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("build failed: %v", err)
	}

	return string(output), nil
}