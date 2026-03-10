// backend/internal/build.go
package internal

import (
	"fmt"
	"os/exec"
)

// RunBuildx executes the Docker buildx command in the specified directory
func RunBuildx(workDir, dockerfile, context string, tags []string, push bool) (string, error) {
	args := []string{"buildx", "build"}

	// Specify the Dockerfile relative to the context
	if dockerfile != "" && dockerfile != "Dockerfile" {
		args = append(args, "-f", dockerfile)
	}

	for _, tag := range tags {
		args = append(args, "-t", tag)
	}

	if push {
		args = append(args, "--push")
	}

	// Append the build context
	if context == "" {
		context = "."
	}
	args = append(args, context)

	cmd := exec.Command("docker", args...)
	cmd.Dir = workDir

	// Capture CombinedOutput for the logs
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("build failed: %v", err)
	}

	return string(output), nil
}