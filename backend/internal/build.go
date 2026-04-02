// backend/internal/build.go
package internal

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RunBuildx executes the Docker buildx command in the specified directory
func RunBuildx(workDir, dockerfile, buildcontext string, tags []string, push bool, noCache bool, out io.Writer) error {
	args := []string{"buildx", "build"}
	args = append(args, "--progress=plain")

	if dockerfile != "" && dockerfile != "Dockerfile" {
		// Ensure Dockerfile path is absolute relative to the cloned git repo root
		args = append(args, "-f", filepath.Join(workDir, dockerfile))
	}

	if noCache {
		args = append(args, "--no-cache")
	}

	for _, tag := range tags {
		args = append(args, "-t", tag)
	}

	if push {
		args = append(args, "--push")
	}

	if buildcontext == "" {
		buildcontext = "."
	}
	args = append(args, buildcontext)

	cmdString := fmt.Sprintf("docker %s\n", strings.Join(args, " "))
	out.Write([]byte(fmt.Sprintf("\nEXEC: %s\n", cmdString)))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cmd := exec.Command("docker", args...)
	cmd.Dir = workDir
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("build timed out after 30 minutes")
		}
		return fmt.Errorf("build failed: %v", err)
	}

	return nil
}

// TagExistingImage uses buildx imagetools to alias an existing image in the remote registry
func TagExistingImage(sourceImage, newTag string) error {
	// e.g., docker buildx imagetools create oci.jell0.online/app:0.1.2 -t oci.jell0.online/app:stable
	cmd := exec.Command("docker", "buildx", "imagetools", "create", sourceImage, "-t", newTag)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remote tagging failed: %v\nOutput: %s", err, string(output))
	}
	
	return nil
}