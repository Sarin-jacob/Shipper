// backend/internal/registry.go
package internal

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"database/sql"
)

// ApplyRetentionPolicy keeps 'latest' and the highest patch of each minor version
func ApplyRetentionPolicy(db *sql.DB, projectID int, registryURL, repository string, currentVersions []string, policy string) error {
	// If policy is "all", we skip cleanup entirely
	if policy == "all" {
		return nil
	}

	keepMap := make(map[string]bool)
	keepMap["latest"] = true

	highestPatches := make(map[string]string)
	for _, v := range currentVersions {
		if strings.HasPrefix(v, "commit-") || v == "latest" {
			continue 
		}

		parts := strings.Split(v, ".")
		if len(parts) >= 2 {
			minorGroup := parts[0] + "." + parts[1]
			currentHighest, exists := highestPatches[minorGroup]
			if !exists || v > currentHighest {
				highestPatches[minorGroup] = v
			}
		}
	}

	for _, v := range highestPatches {
		keepMap[v] = true
	}

	for _, v := range currentVersions {
		if !keepMap[v] && !strings.HasPrefix(v, "commit-") {
			fmt.Printf("Untagging old version: %s:%s\n", repository, v)
			
			err := deleteRegistryTag(registryURL, repository, v)
			if err != nil {
				// If it's a 404, it's already gone from the registry! We just need to sync the DB.
				if strings.Contains(err.Error(), "404") {
					fmt.Printf("Tag %s already missing from registry. Syncing DB.\n", v)
				} else {
					fmt.Printf("Failed to delete tag %s: %v\n", v, err)
					continue
				}
			}

			// Sync the Database! Change status so UI knows it was cleaned up
			db.Exec("UPDATE builds SET status = 'archived' WHERE version = ? AND project_id = ?", v, projectID)
			db.Exec("DELETE FROM tags WHERE build_id = (SELECT id FROM builds WHERE version = ? AND project_id = ?)", v, projectID)
		}
	}

	return nil
}

func deleteRegistryTag(registryURL, repository, tag string) error {
	client := &http.Client{}
	repoName := repository
	prefix := registryURL + "/"
	if after, ok :=strings.CutPrefix(repoName, prefix); ok  {
		repoName = after
	}
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registryURL, repoName, tag)
	
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry returned status: %d", resp.StatusCode)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return fmt.Errorf("no digest header returned")
	}

	deleteUrl := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registryURL, repoName, digest)
	delReq, _ := http.NewRequest("DELETE", deleteUrl, nil)
	
	delResp, err := client.Do(delReq)
	if err != nil {
		return err
	}
	defer delResp.Body.Close()

	if delResp.StatusCode != http.StatusAccepted && delResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete, status: %d", delResp.StatusCode)
	}

	return nil
}

func RunGarbageCollection(containerName string) error {
	fmt.Println("Triggering Registry Garbage Collection...")

	gcScript := `
		if [ -f /etc/distribution/config.yml ]; then
			bin/registry garbage-collect /etc/distribution/config.yml --delete-untagged
		elif [ -f /etc/docker/registry/config.yml ]; then
			bin/registry garbage-collect /etc/docker/registry/config.yml --delete-untagged
		else
			echo "Could not find registry config file"
			exit 1
		fi
	`

	cmd := exec.Command("docker", "exec", containerName, "sh", "-c", gcScript)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("garbage collection failed: %v\nOutput: %s", err, string(output))
	}

	fmt.Println("Garbage Collection complete!")
	return nil
}