// backend/internal/pipeline.go
package internal

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// ExecuteBuild runs the full CI/CD pipeline
func ExecuteBuild(db *sql.DB, cfg Config, projectID int) error {
	log.Printf("[Project %d] Starting build pipeline...", projectID)

	var name, repoURL, branch, imageName string
	err := db.QueryRow("SELECT name, repo_url, branch, image_name FROM projects WHERE id = ?", projectID).
		Scan(&name, &repoURL, &branch, &imageName)
	if err != nil {
		return fmt.Errorf("failed to fetch project: %v", err)
	}

	var currentVersion, lastCommit string
	db.QueryRow("SELECT last_version, last_commit_built FROM state WHERE project_id = ?", projectID).
		Scan(&currentVersion, &lastCommit)

	// Create temp workspace
	workDir, err := os.MkdirTemp("", fmt.Sprintf("shiper-build-%d-*", projectID))
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(workDir)

	// Clone & Check Commit
	if err := CloneRepo(repoURL, branch, workDir); err != nil {
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
	newVersion, err := IncrementPatch(currentVersion)
	if err != nil {
		return err
	}
	
	tags := GenerateTags(imageName, newVersion, commitHash, []string{})

	// Record start
	buildID := recordBuildStart(db, projectID, newVersion, commitHash)

	// Execute Build
	log.Printf("[Project %d] Building %s context (%s)...", projectID, target.Type, target.File)
	logs, buildErr := RunBuildx(workDir, target.Dockerfile, target.Context, tags, true)

	// Finalize
	logsPath := saveLogs(cfg.DataDir, projectID, buildID, logs)
	status := "success"
	if buildErr != nil {
		status = "failed"
	} else {
		updateState(db, projectID, newVersion, commitHash)
		
		// Run retention policy asynchronously
		go func() {
			allVersions := fetchAllVersions(db, projectID)
			ApplyRetentionPolicy(cfg.RegistryURL, imageName, allVersions)
		}()
	}

	recordBuildFinish(db, buildID, status, logsPath)
	return buildErr
}

// --- DB Helpers ---

func recordBuildStart(db *sql.DB, projectID int, version, commit string) int64 {
	res, _ := db.Exec(`INSERT INTO builds (project_id, version, commit_hash, status, started_at) VALUES (?, ?, ?, 'building', ?)`, projectID, version, commit, time.Now())
	id, _ := res.LastInsertId()
	return id
}

func recordBuildFinish(db *sql.DB, buildID int64, status, logsPath string) {
	db.Exec(`UPDATE builds SET status = ?, finished_at = ?, logs_path = ? WHERE id = ?`, status, time.Now(), logsPath, buildID)
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