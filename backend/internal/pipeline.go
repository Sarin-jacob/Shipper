package internal

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExecuteBuild runs the full CI/CD pipeline for a given project ID
func ExecuteBuild(db *sql.DB, projectID int) error {
	log.Printf("[Project %d] Starting build pipeline...", projectID)

	// 1. Fetch project details
	var name, repoURL, branch, imageName string
	err := db.QueryRow("SELECT name, repo_url, branch, image_name FROM projects WHERE id = ?", projectID).
		Scan(&name, &repoURL, &branch, &imageName)
	if err != nil {
		return fmt.Errorf("failed to fetch project: %v", err)
	}

	// 2. Fetch current state (version and last commit)
	var currentVersion, lastCommit string
	err = db.QueryRow("SELECT last_version, last_commit_built FROM state WHERE project_id = ?", projectID).
		Scan(&currentVersion, &lastCommit)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to fetch state: %v", err)
	}

	// 3. Create a temporary build workspace
	workDir, err := os.MkdirTemp("", fmt.Sprintf("shiper-build-%d-*", projectID))
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(workDir) // Cleanup after build

	// 4. Clone the repository
	log.Printf("[Project %d] Cloning %s (branch: %s)...", projectID, repoURL, branch)
	if err := Clone(repoURL, branch, workDir); err != nil {
		return err
	}

	// 5. Get the latest commit hash from the cloned repo
	commitHash, err := getCommitHash(workDir)
	if err != nil {
		return err
	}

	if commitHash == lastCommit {
		log.Printf("[Project %d] Commit %s already built. Skipping.", projectID, commitHash[:7])
		return nil
	}

	// 6. Calculate new version and generate tags
	newVersion, err := IncrementPatch(currentVersion)
	if err != nil {
		return err
	}
	
	// Assuming no custom tags for this basic run, but we can fetch them from DB later
	tags := GenerateTags(imageName, newVersion, commitHash, []string{})

	// 7. Record build start in DB
	buildID := recordBuildStart(db, projectID, newVersion, commitHash)

	// 8. Execute Docker Buildx
	log.Printf("[Project %d] Building and pushing tags: %v", projectID, tags)
	
	// For simplicity, assuming standard context "." and "Dockerfile". 
	// If it's a compose file, you would call `detect.AnalyzeRepo` here to get the specific context/file.
	logs, buildErr := RunBuildx(workDir, "Dockerfile", ".", tags, true)

	// ... inside ExecuteBuild in pipeline.go ...

	if buildErr != nil {
		status := "failed"
		log.Printf("[Project %d] Build failed: %v", projectID, buildErr)
	} else {
		log.Printf("[Project %d] Build successful! Version: %s", projectID, newVersion)
		updateState(db, projectID, newVersion, commitHash)
		
		// Run the retention policy cleanup!
		// (In a real scenario, you'd fetch 'allVersions' from your DB or directly from the registry API)
		allVersions := fetchAllVersionsFromDB(db, projectID)
		go ApplyRetentionPolicy(cfg.RegistryURL, imageName, allVersions) 
	}
	// 9. Save logs to disk
	logsPath := saveLogs(projectID, buildID, logs)

	// 10. Finalize state in DB
	status := "success"
	if buildErr != nil {
		status = "failed"
		log.Printf("[Project %d] Build failed: %v", projectID, buildErr)
	} else {
		log.Printf("[Project %d] Build successful! Version: %s", projectID, newVersion)
		updateState(db, projectID, newVersion, commitHash)
	}

	recordBuildFinish(db, buildID, status, logsPath)

	return buildErr
}

// --- Helper Functions ---

func getCommitHash(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get commit hash: %v", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func recordBuildStart(db *sql.DB, projectID int, version, commit string) int64 {
	res, _ := db.Exec(`
		INSERT INTO builds (project_id, version, commit_hash, status, started_at) 
		VALUES (?, ?, ?, 'building', ?)`,
		projectID, version, commit, time.Now(),
	)
	id, _ := res.LastInsertId()
	return id
}

func recordBuildFinish(db *sql.DB, buildID int64, status, logsPath string) {
	db.Exec(`
		UPDATE builds SET status = ?, finished_at = ?, logs_path = ? WHERE id = ?`,
		status, time.Now(), logsPath, buildID,
	)
}

func updateState(db *sql.DB, projectID int, version, commit string) {
	db.Exec(`
		INSERT INTO state (project_id, last_version, last_commit_built) 
		VALUES (?, ?, ?) 
		ON CONFLICT(project_id) DO UPDATE SET 
		last_version = excluded.last_version, 
		last_commit_built = excluded.last_commit_built`,
		projectID, version, commit,
	)
}

func saveLogs(projectID int, buildID int64, logs string) string {
	logDir := filepath.Join("data", "logs", fmt.Sprintf("project_%d", projectID))
	os.MkdirAll(logDir, os.ModePerm)
	
	logPath := filepath.Join(logDir, fmt.Sprintf("build_%d.log", buildID))
	os.WriteFile(logPath, []byte(logs), 0644)
	
	return logPath
}