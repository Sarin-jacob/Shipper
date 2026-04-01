// backend/internal/poller.go
package internal

import (
	"database/sql"
	"log"
	"time"
)

// Scheduler manages the background polling job
type Scheduler struct {
	db       *sql.DB
	cfg      Config
	interval time.Duration
}

func NewScheduler(db *sql.DB, cfg Config) *Scheduler {
	return &Scheduler{
		db:       db,
		cfg:      cfg,
		interval: cfg.PollInterval,
	}
}

// Start begins the polling loop
func (s *Scheduler) Start() {
	ticker := time.NewTicker(s.interval)
	log.Printf("Scheduler started. Polling every %v", s.interval)

	go func() {
		for range ticker.C {
			s.pollProjects()
		}
	}()
}

func (s *Scheduler) pollProjects() {
	query := `
		SELECT p.id, p.repo_url, p.branch 
		FROM projects p 
		LEFT JOIN builds b ON p.id = b.project_id AND b.status = 'building'
		WHERE p.enabled = 1 AND b.id IS NULL
	`
	rows, err := s.db.Query(query)
	if err != nil {
		log.Printf("Polling error fetching projects: %v", err)
		return
	}
	defer rows.Close()
	settings := LoadSettings(s.cfg.DataDir)
	for rows.Next() {
		var id int
		var repoURL, branch string
		if err := rows.Scan(&id, &repoURL, &branch); err != nil {
			continue
		}

		// Check remote commit without cloning
		remoteCommit, err := GetRemoteCommitHash(repoURL, branch, settings.GHToken)
		if err != nil {
			log.Printf("[Project %d] Failed to check remote commit: %v", id, err)
			continue
		}

		// Compare with state
		var lastCommit string
		err = s.db.QueryRow("SELECT last_commit_built FROM state WHERE project_id = ?", id).Scan(&lastCommit)
		
		if err == sql.ErrNoRows || remoteCommit != lastCommit {
			log.Printf("[Project %d] Update detected! Triggering build.", id)
			go func(projectID int) {
				if err := ExecuteBuild(s.db, s.cfg, projectID, false, false); err != nil {
					log.Printf("[Project %d] Automated build failed: %v", projectID, err)
				}
			}(id)
		}
	}
}