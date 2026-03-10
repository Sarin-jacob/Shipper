package internal

import (
	"fmt"
	"net/http"
	// "sort"
	"strings"
)

// ApplyRetentionPolicy is called after a successful build.
// It fetches all tags, figures out which ones to delete, and deletes them.
func ApplyRetentionPolicy(registryURL, repository string, currentVersions []string) error {
	// 1. Figure out which tags we MUST keep based on your Section 5 rules
	keepMap := make(map[string]bool)
	keepMap["latest"] = true

	highestPatches := make(map[string]string)
	for _, v := range currentVersions {
		// Ignore commit tags or non-semver tags for the patch calculation
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

	// 2. Delete the ones that didn't make the cut
	for _, v := range currentVersions {
		if !keepMap[v] && !strings.HasPrefix(v, "commit-") { // Decide if you want to keep commit hashes or delete them too
			fmt.Printf("Untagging old version: %s:%s\n", repository, v)
			if err := deleteRegistryTag(registryURL, repository, v); err != nil {
				fmt.Printf("Failed to delete tag %s: %v\n", v, err)
			}
		}
	}

	return nil
}

// deleteRegistryTag interacts with the OCI Distribution Specification API
func deleteRegistryTag(registryURL, repository, tag string) error {
	client := &http.Client{}

	// Step 1: GET the manifest to find the Docker-Content-Digest header
	// OCI registries require you to delete by Digest, not by Tag name.
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registryURL, repository, tag)
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return err
	}
	
	// We need the v2 manifest schema to get the correct digest
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	// TODO: Add Authorization header here if oci.jell0.online requires basic auth/bearer token
	
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
		return fmt.Errorf("no digest header returned from registry")
	}

	// Step 2: DELETE the manifest using the digest
	deleteUrl := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registryURL, repository, digest)
	delReq, _ := http.NewRequest("DELETE", deleteUrl, nil)
	
	// TODO: Add Auth header here too
	
	delResp, err := client.Do(delReq)
	if err != nil {
		return err
	}
	defer delResp.Body.Close()

	if delResp.StatusCode != http.StatusAccepted && delResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete, registry returned: %d", delResp.StatusCode)
	}

	return nil
}