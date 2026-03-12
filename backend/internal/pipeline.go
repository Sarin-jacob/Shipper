// backend/internal/pipeline.go
package internal

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
	"strings"
	"sync"
)

var buildLocks sync.Map

// ExecuteBuild runs the full CI/CD pipeline
func ExecuteBuild(db *sql.DB, cfg Config, projectID int) error {
	// --- ANTI-RACE CONDITION LOCK ---
	// var isBuilding bool
	// err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM builds WHERE project_id = ? AND status = 'building')", projectID).Scan(&isBuilding)
	// if err == nil && isBuilding {
	// 	log.Printf("[Project %d] Build already in progress. Skipping concurrent trigger.", projectID)
	// 	return nil
	// }
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
	var name, repoURL, branch, imageName, customTagsStr, registryOverride string
	err := db.QueryRow("SELECT name, repo_url, branch, image_name, COALESCE(custom_tags, ''), COALESCE(registry_override, '') FROM projects WHERE id = ?", projectID).
		Scan(&name, &repoURL, &branch, &imageName, &customTagsStr, &registryOverride)
	if err != nil {
		return fmt.Errorf("failed to fetch project: %v", err)
	}

	// Override registry if set
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

	// Clone & Check Commit
	if err := CloneRepo(repoURL, branch, workDir,settings.GHToken); err != nil {
		return err
	}

	commitHash, err := GetLocalCommitHash(workDir)
	if err != nil {
		return err
	}

	if commitHash == lastCommit {
		log.Printf("[Project %d] Commit %s already built. Skipping.", projectID, commitHash[:7])
		return nil
	}

	// Analyze Build Config (Compose vs Dockerfile)
	target, err := AnalyzeRepo(workDir)
	if err != nil {
		return fmt.Errorf("analysis failed: %v", err)
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
	log.Printf("[Project %d] Building %s context (%s)...", projectID, target.Type, target.File)
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
	logFile.Write([]byte("---------------------------------------------------\n"))

	// Execute Build (Pass the logFile stream)
	log.Printf("[Project %d] Building %s context (%s)...", projectID, target.Type, target.File)
	buildErr := RunBuildx(workDir, target.Dockerfile, target.Context, tags, true, logFile)
	
	logFile.Close()

	status := "success"
	if buildErr != nil {
		status = "failed"
		// SAFELY APPEND the error instead of truncating!
		f, _ := os.OpenFile(logsPath, os.O_APPEND|os.O_WRONLY, 0666)
		if f != nil {
			f.Write([]byte(fmt.Sprintf("\n\n--- SYSTEM ERROR ---\n%v\n", buildErr)))
			f.Close()
		}
	} else {
		updateState(db, projectID, newVersion, commitHash)
		db.Exec("UPDATE state SET next_bump = 'patch' WHERE project_id = ?", projectID)

		db.Exec("DELETE FROM tags WHERE tag = 'latest' AND build_id IN (SELECT id FROM builds WHERE project_id = ?)", projectID)

		// Save tags to DB so they show up in UI
		for _, fullTag := range tags {
			parts := strings.Split(fullTag, ":")
			if len(parts) >= 2 {
				db.Exec("INSERT INTO tags (build_id, tag) VALUES (?, ?)", buildID, parts[len(parts)-1])
			}
		}
	}

	recordBuildFinish(db, buildID, status)
	BroadcastEvent("update")

	if status == "success"{
		// Run retention policy asynchronously
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