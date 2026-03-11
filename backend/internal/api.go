// backend/internal/api.go
package internal

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
)

// Server holds the database connection and configuration
type Server struct {
	db  *sql.DB
	cfg Config
}

// NewServer initializes a new API server instance
func NewServer(db *sql.DB, cfg Config) *Server {
	return &Server{
		db:  db,
		cfg: cfg,
	}
}

// SetupRoutes registers the API endpoints (Requires Go 1.22+ for method routing)
func (s *Server) SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Project Management
	mux.HandleFunc("GET /api/projects", s.handleGetProjects)
	mux.HandleFunc("POST /api/projects", s.handleAddProject)

	// Build Pipeline Operations
	mux.HandleFunc("POST /api/projects/{id}/build", s.handleTriggerBuild)
	mux.HandleFunc("GET /api/projects/{id}/builds", s.handleGetBuilds)
	mux.HandleFunc("GET /api/builds/{id}/logs", s.handleGetLogs)
	mux.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)
	mux.HandleFunc("DELETE /api/builds/{id}", s.handleDeleteBuild)
	mux.HandleFunc("PUT /api/projects/{id}/tags", s.handleUpdateProjectTags)

	fileServer := http.FileServer(http.Dir(s.cfg.StaticDir))
	mux.Handle("/", fileServer)

	return mux
}

// --- Handlers ---

func (s *Server) handleGetProjects(w http.ResponseWriter, r *http.Request) {
	// We join with the state table to get the latest version and status
	query := `
		SELECT p.id, p.name, p.repo_url, p.branch, 
		       COALESCE(st.last_version, 'Unknown') as version,
			   (SELECT status FROM builds WHERE project_id = p.id ORDER BY id DESC LIMIT 1) as status
		FROM projects p
		LEFT JOIN state st ON p.id = st.project_id
	`
	rows, err := s.db.Query(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		var status sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoURL, &p.Branch, &p.Version, &status); err == nil {
			if status.Valid {
				p.Status = status.String
			} else {
				p.Status = "pending"
			}
			projects = append(projects, p)
		}
	}

	if projects == nil {
		projects = []Project{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

func (s *Server) handleAddProject(w http.ResponseWriter, r *http.Request) {
	var p Project
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Default image name based on registry and project name if not provided
	if p.ImageName == "" {
		p.ImageName = s.cfg.RegistryURL + "/" + p.Name
	}

	result, err := s.db.Exec(`
		INSERT INTO projects (name, repo_url, branch, image_name, enabled) 
		VALUES (?, ?, ?, ?, ?)`,
		p.Name, p.RepoURL, p.Branch, p.ImageName, true, // Auto-enable for MVP
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	p.ID = int(id)
	
	s.db.Exec("INSERT INTO state (project_id, last_version, last_commit_built) VALUES (?, '', '')", id)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(p)
}

func (s *Server) handleTriggerBuild(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	projectID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	go func() {
		if err := ExecuteBuild(s.db, s.cfg, projectID); err != nil {
			log.Printf("Manual build failed for project %d: %v", projectID, err)
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
		http.Error(w, "Build logs not found", http.StatusNotFound)
		return
	}

	file, err := os.Open(logsPath)
	if err != nil {
		http.Error(w, "Could not read log file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "text/plain")
	io.Copy(w, file)
}

// --- Cleanup & Settings Handlers ---

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	// 1. Delete physical log files
	logDir := s.cfg.DataDir + "/logs/project_" + projectID
	os.RemoveAll(logDir)

	// 2. Delete from DB (Delete child rows first to avoid orphans)
	s.db.Exec("DELETE FROM tags WHERE build_id IN (SELECT id FROM builds WHERE project_id = ?)", projectID)
	s.db.Exec("DELETE FROM builds WHERE project_id = ?", projectID)
	s.db.Exec("DELETE FROM state WHERE project_id = ?", projectID)
	s.db.Exec("DELETE FROM projects WHERE id = ?", projectID)

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteBuild(w http.ResponseWriter, r *http.Request) {
	buildID := r.PathValue("id")

	// 1. Fetch log path and delete the file
	var logsPath string
	s.db.QueryRow("SELECT logs_path FROM builds WHERE id = ?", buildID).Scan(&logsPath)
	if logsPath != "" {
		os.Remove(logsPath)
	}

	// 2. Delete from DB
	s.db.Exec("DELETE FROM tags WHERE build_id = ?", buildID)
	s.db.Exec("DELETE FROM builds WHERE id = ?", buildID)

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleUpdateProjectTags(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	
	var payload struct {
		Tags string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	s.db.Exec("UPDATE projects SET custom_tags = ? WHERE id = ?", payload.Tags, projectID)
	w.WriteHeader(http.StatusOK)
}