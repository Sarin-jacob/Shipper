package internal

import (
	"database/sql"
	"log"
	"time"
)

type Scheduler struct {
	db       *sql.DB
	interval time.Duration
}

func NewScheduler(db *sql.DB, interval time.Duration) *Scheduler {
	return &Scheduler{
		db:       db,
		interval: interval,
	}
}

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
	// 1. Fetch all enabled projects from DB
	// SELECT id, repo_url, branch FROM projects WHERE enabled = 1

	// 2. For each project, run `git ls-remote` to get the latest commit hash on that branch
	// We don't need to clone the whole repo just to check if there's an update!
	// exec.Command("git", "ls-remote", repoURL, branch)

	// 3. Compare with `last_commit_built` in the `state` table

	// 4. If different -> Trigger the Build Pipeline!
	//   - Update status to "building"
	//   - Clone
	//   - RunBuildx
	//   - Update version/commit in DB
	//   - Save logs

	log.Println("Polling cycle complete...")
}