// backend/internal/api.go
package internal

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"io"
	"os"
	"strconv"
)

type Server struct {
	db *sql.DB
}

func NewServer(db *sql.DB) *Server {
	return &Server{db: db}
}

// SetupRoutes registers our API endpoints (Requires Go 1.22+ for method routing)
func (s *Server) SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Project Management
	mux.HandleFunc("GET /api/projects", s.handleGetProjects)
	mux.HandleFunc("POST /api/projects", s.handleAddProject)

	// Build Pipeline
	mux.HandleFunc("POST /api/projects/{id}/build", s.handleTriggerBuild)
	mux.HandleFunc("GET /api/projects/{id}/builds", s.handleGetBuilds)
	mux.HandleFunc("GET /api/builds/{id}/logs", s.handleGetLogs)

	return mux
}

// Project represents the data sent to the UI
type Project struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	RepoURL string `json:"repo_url"`
	Branch  string `json:"branch"`
	// We'll mock status and version for the UI until the build pipeline is ready
	Status  string `json:"status"`
	Version string `json:"version"`
}

func (s *Server) handleGetProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query("SELECT id, name, repo_url, branch FROM projects")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoURL, &p.Branch); err != nil {
			log.Printf("Error scanning project: %v", err)
			continue
		}
		p.Status = "up to date" // Mocked for now
		p.Version = "0.1.0"     // Mocked for now
		projects = append(projects, p)
	}

	// Return empty array instead of null if no projects exist
	if projects == nil {
		projects = []Project{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

func (s *Server) handleAddProject(w http.ResponseWriter, r *http.Request) {
	var p Project // Using the struct we defined earlier
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := s.db.Exec(`
		INSERT INTO projects (name, repo_url, branch, image_name) 
		VALUES (?, ?, ?, ?)`,
		p.Name, p.RepoURL, p.Branch, p.Name, // Using name as default image_name for now
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	p.ID = int(id)
	
	// Initialize empty state
	s.db.Exec("INSERT INTO state (project_id, last_version, last_commit_built) VALUES (?, '', '')", id)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(p)
}

// --- Build Pipeline ---

func (s *Server) handleTriggerBuild(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	projectID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	// Run the build in a goroutine so the HTTP request doesn't hang 
	// while Docker is compiling the image.
	go func() {
		err := ExecuteBuild(s.db, projectID)
		if err != nil {
			log.Printf("Background build failed for project %d: %v", projectID, err)
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status": "Build triggered successfully"}`))
}

type BuildHistory struct {
	ID         int    `json:"id"`
	Version    string `json:"version"`
	CommitHash string `json:"commit_hash"`
	Status     string `json:"status"`
	FinishedAt string `json:"finished_at"`
}

func (s *Server) handleGetBuilds(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	
	rows, err := s.db.Query(`
		SELECT id, version, commit_hash, status, COALESCE(finished_at, '') 
		FROM builds WHERE project_id = ? ORDER BY id DESC`, projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var builds []BuildHistory
	for rows.Next() {
		var b BuildHistory
		if err := rows.Scan(&b.ID, &b.Version, &b.CommitHash, &b.Status, &b.FinishedAt); err == nil {
			builds = append(builds, b)
		}
	}

	if builds == nil {
		builds = []BuildHistory{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(builds)
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	buildID := r.PathValue("id")
	
	var logsPath string
	err := s.db.QueryRow("SELECT logs_path FROM builds WHERE id = ?", buildID).Scan(&logsPath)
	if err != nil {
		http.Error(w, "Build logs not found in database", http.StatusNotFound)
		return
	}

	if logsPath == "" {
		http.Error(w, "Logs not yet available", http.StatusNotFound)
		return
	}

	file, err := os.Open(logsPath)
	if err != nil {
		http.Error(w, "Could not read log file from disk", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "text/plain")
	io.Copy(w, file)
}