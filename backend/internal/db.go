// backend/internal/db.go
package internal

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

// Project represents the core project entity
type Project struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	RepoURL     string `json:"repo_url"`
	Branch      string `json:"branch"`
	ImageName   string `json:"image_name"`
	ComposeFile string `json:"compose_file"`
	ServiceName string `json:"service_name"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
	// Joined fields for UI convenience
	Status  string `json:"status"`
	Version string `json:"version"`
}

// InitDB creates the connection and ensures our schema exists
func InitDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS projects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		repo_url TEXT NOT NULL,
		branch TEXT NOT NULL,
		image_name TEXT,
		compose_file TEXT,
		service_name TEXT,
		enabled BOOLEAN DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS builds (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER,
		version TEXT,
		commit_hash TEXT,
		status TEXT,
		started_at DATETIME,
		finished_at DATETIME,
		logs_path TEXT,
		FOREIGN KEY(project_id) REFERENCES projects(id)
	);

	CREATE TABLE IF NOT EXISTS tags (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		build_id INTEGER,
		tag TEXT,
		FOREIGN KEY(build_id) REFERENCES builds(id)
	);

	CREATE TABLE IF NOT EXISTS state (
		project_id INTEGER PRIMARY KEY,
		last_commit_built TEXT,
		last_version TEXT,
		FOREIGN KEY(project_id) REFERENCES projects(id)
	);
	`

	if _, err = db.Exec(schema); err != nil {
		return nil, err
	}

	log.Println("✅ SQLite database initialized.")
	return db, nil
}