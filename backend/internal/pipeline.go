// backend/internal/pipeline.go
package internal

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var buildLocks sync.Map

// ExecuteBuild runs the full CI/CD pipeline
func ExecuteBuild(db *sql.DB, cfg Config, projectID int, isManual bool, noCache bool) error {
	lockObj, _ := buildLocks.LoadOrStore(projectID, &sync.Mutex{})
	mutex := lockObj.(*sync.Mutex)

	if !mutex.TryLock() {
		log.Printf("[Project %d] Build already in progress. Ignoring duplicate trigger.", projectID)
		return nil
	}
	defer mutex.Unlock()

	log.Printf("[Project %d] Starting build pipeline...", projectID)
	settings := LoadSettings(cfg.DataDir)
	// 1. Fetch project details
	var name, repoURL, branch, imageName, customTagsStr, registryOverride, targetService string
	err := db.QueryRow("SELECT name, repo_url, branch, image_name, COALESCE(custom_tags, ''), COALESCE(registry_override, ''), COALESCE(service_name, '') FROM projects WHERE id = ?", projectID).
		Scan(&name, &repoURL, &branch, &imageName, &customTagsStr, &registryOverride, &targetService)
	if err != nil {
		return fmt.Errorf("failed to fetch project: %v", err)
	}

	if registryOverride != "" {
		imageName = registryOverride + "/" + name
	}

	var currentVersion, lastCommit, nextBump string
	db.QueryRow("SELECT last_version, last_commit_built, COALESCE(next_bump, 'patch') FROM state WHERE project_id = ?", projectID).
		Scan(&currentVersion, &lastCommit, &nextBump)

	// Create temp workspace
	workDir, err := os.MkdirTemp("", fmt.Sprintf("shipper-build-%d-*", projectID))
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(workDir)

	if err := CloneRepo(repoURL, branch, workDir, settings.GHToken); err != nil {
		return err
	}

	commitHash, err := GetLocalCommitHash(workDir)
	if err != nil {
		return err
	}

	if !isManual && commitHash == lastCommit {
		log.Printf("[Project %d] Commit %s already built. Skipping.", projectID, commitHash[:7])
		return nil
	}

	// 2. Analyze Build Config (Now returns an ARRAY of targets)
	targets, err := AnalyzeRepo(workDir)
	if err != nil || len(targets) == 0 {
		return fmt.Errorf("analysis failed: %v", err)
	}

	var activeTarget BuildTarget

	// --- 3. THE AUTO-SPLIT LOGIC ---
	if targetService != "" {
		// This project already knows what service it's targeting
		for _, t := range targets {
			if t.ServiceName == targetService {
				activeTarget = t
				break
			}
		}
		if activeTarget.Type == "" {
			return fmt.Errorf("target service '%s' not found in repo", targetService)
		}
	} else {
		// This is a fresh project with no target service defined yet
		if len(targets) == 1 {
			activeTarget = targets[0]
			// Save the target service so we remember it next time
			db.Exec("UPDATE projects SET service_name = ? WHERE id = ?", activeTarget.ServiceName, projectID)
		} else {
			log.Printf("[Project %d] Multiple services found. Auto-generating child projects...", projectID)
			
			originalName := name
			originalImageName := imageName

			// A. Assign the FIRST service to the CURRENT project
			activeTarget = targets[0]
			name = fmt.Sprintf("%s-%s", originalName, activeTarget.ServiceName)
			imageName = fmt.Sprintf("%s-%s", originalImageName, activeTarget.ServiceName)
			
			db.Exec("UPDATE projects SET name = ?, image_name = ?, service_name = ? WHERE id = ?", name, imageName, activeTarget.ServiceName, projectID)

			// B. Spawn BRAND NEW projects in the DB for the remaining services
			for i := 1; i < len(targets); i++ {
				t := targets[i]
				tName := fmt.Sprintf("%s-%s", originalName, t.ServiceName)
				tImageName := fmt.Sprintf("%s-%s", originalImageName, t.ServiceName)

				res, err := db.Exec(`
					INSERT INTO projects (name, repo_url, branch, image_name, custom_tags, registry_override, service_name, enabled) 
					VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
					tName, repoURL, branch, tImageName, customTagsStr, registryOverride, t.ServiceName)

				// Fire off background builds for the newly created projects!
				if err == nil {
					newProjID, _ := res.LastInsertId()
					go ExecuteBuild(db, cfg, int(newProjID), false, false) 
				}
			}
			BroadcastEvent("update") // Tell the UI about the new projects!
		}
	}

	// Calculate Version
	var newVersion string
	switch nextBump {
	case "major":
		newVersion, _ = BumpMajor(currentVersion)
	case "minor":
		newVersion, _ = BumpMinor(currentVersion)
	default:
		newVersion, _ = IncrementPatch(currentVersion)
	}

	var customTags []string
	if customTagsStr != "" {
		for _, t := range strings.Split(customTagsStr, ",") {
			customTags = append(customTags, strings.TrimSpace(t))
		}
	}
	
	tags := GenerateTags(imageName, newVersion, commitHash, customTags)

	// Record start
	buildID := recordBuildStart(db, projectID, newVersion, commitHash)
	BroadcastEvent("update")

	// Execute Build
	logDir := filepath.Join(cfg.DataDir, "logs", fmt.Sprintf("project_%d", projectID))
	os.MkdirAll(logDir, os.ModePerm)
	logsPath := filepath.Join(logDir, fmt.Sprintf("build_%d.log", buildID))

	logFile, err := os.Create(logsPath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %v", err)
	}

	db.Exec(`UPDATE builds SET logs_path = ? WHERE id = ?`, logsPath, buildID)

	logFile.Write([]byte(fmt.Sprintf("Starting Shipper Build Pipeline for %s...\n", name)))
	logFile.Write([]byte(fmt.Sprintf("Commit: %s | Target Version: %s\n", commitHash[:7], newVersion)))
	if activeTarget.ServiceName != "" {
		logFile.Write([]byte(fmt.Sprintf("Target Service: %s\n", activeTarget.ServiceName)))
	}
	if noCache {
		logFile.Write([]byte("WARNING: Cache disabled for this build (--no-cache)\n"))
	}
	logFile.Write([]byte("---------------------------------------------------\n"))

	// Pass the context (like ./backend) to RunBuildx
	log.Printf("[Project %d] Building %s context (%s)...", projectID, activeTarget.Type, activeTarget.File)
	buildErr := RunBuildx(workDir, activeTarget.Dockerfile, activeTarget.Context, tags, true, noCache, logFile)
	logFile.Close()

	if buildErr == nil {
		go func(cleanupTags []string) {
			for _, t := range cleanupTags {
				cmd := exec.Command("docker", "rmi", t)
				if err := cmd.Run(); err != nil {
					log.Printf("Note: Failed to remove local image cache %s: %v", t, err)
				}
			}
		}(tags)
	}

	// ... [THE REST OF FINALIZE REMAINS EXACTLY THE SAME] ...
	status := "success"
	if buildErr != nil {
		status = "failed"
		f, _ := os.OpenFile(logsPath, os.O_APPEND|os.O_WRONLY, 0666)
		if f != nil {
			f.Write([]byte(fmt.Sprintf("\n\n--- SYSTEM ERROR ---\n%v\n", buildErr)))
			f.Close()
		}
	} else {
		updateState(db, projectID, newVersion, commitHash)
		db.Exec("UPDATE state SET next_bump = 'patch' WHERE project_id = ?", projectID)
		db.Exec("DELETE FROM tags WHERE tag = 'latest' AND build_id IN (SELECT id FROM builds WHERE project_id = ?)", projectID)

		for _, fullTag := range tags {
			parts := strings.Split(fullTag, ":")
			if len(parts) >= 2 {
				db.Exec("INSERT INTO tags (build_id, tag) VALUES (?, ?)", buildID, parts[len(parts)-1])
			}
		}
	}

	recordBuildFinish(db, buildID, status)
	BroadcastEvent("update")

	if status == "success" {
		go func() {
			allVersions := fetchAllVersions(db, projectID)
			ApplyRetentionPolicy(db, projectID, cfg.RegistryURL, imageName, allVersions, settings.RetentionPolicy)
			if err := RunGarbageCollection(cfg.RegistryContainer); err != nil {
				log.Printf("Registry GC error: %v", err)
			}
		}()
	}

	return buildErr
}

// --- DB Helpers ---

func recordBuildStart(db *sql.DB, projectID int, version, commit string) int64 {
	// 1. Check for an existing FAILED build for this exact commit
	var oldID int
	var oldLogPath string
	err := db.QueryRow("SELECT id, logs_path FROM builds WHERE project_id = ? AND commit_hash = ? AND status = 'failed'", projectID, commit).Scan(&oldID, &oldLogPath)
	
	if err == nil {
		// Found one! Delete the physical log file and the DB records
		os.Remove(oldLogPath)
		db.Exec("DELETE FROM tags WHERE build_id = ?", oldID)
		db.Exec("DELETE FROM builds WHERE id = ?", oldID)
		log.Printf("[Project %d] Removed previous failed build entry for commit %s", projectID, commit[:7])
	}

	// 2. Insert the new build
	res, _ := db.Exec(`INSERT INTO builds (project_id, version, commit_hash, status, started_at) VALUES (?, ?, ?, 'building', ?)`, projectID, version, commit, time.Now())
	id, _ := res.LastInsertId()
	return id
}

func recordBuildFinish(db *sql.DB, buildID int64, status string) {
	db.Exec(`UPDATE builds SET status = ?, finished_at = ? WHERE id = ?`, status, time.Now(), buildID)
}

func updateState(db *sql.DB, projectID int, version, commit string) {
	db.Exec(`INSERT INTO state (project_id, last_version, last_commit_built) VALUES (?, ?, ?) ON CONFLICT(project_id) DO UPDATE SET last_version = excluded.last_version, last_commit_built = excluded.last_commit_built`, projectID, version, commit)
}

func saveLogs(dataDir string, projectID int, buildID int64, logs string) string {
	logDir := filepath.Join(dataDir, "logs", fmt.Sprintf("project_%d", projectID))
	os.MkdirAll(logDir, os.ModePerm)
	logPath := filepath.Join(logDir, fmt.Sprintf("build_%d.log", buildID))
	os.WriteFile(logPath, []byte(logs), 0644)
	return logPath
}

func fetchAllVersions(db *sql.DB, projectID int) []string {
	rows, _ := db.Query("SELECT version FROM builds WHERE project_id = ? AND status = 'success'", projectID)
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err == nil {
			versions = append(versions, v)
		}
	}
	return versions
}