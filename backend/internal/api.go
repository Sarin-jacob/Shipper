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
	"fmt"
	"os/exec"
	"path/filepath"
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

	mux.HandleFunc("GET /api/events", HandleSSE)

	// Project Management
	mux.HandleFunc("GET /api/projects", s.handleGetProjects)
	mux.HandleFunc("POST /api/projects", s.handleAddProject)

	// Build Pipeline Operations
	mux.HandleFunc("POST /api/projects/{id}/build", s.handleTriggerBuild)
	mux.HandleFunc("GET /api/projects/{id}/builds", s.handleGetBuilds)
	mux.HandleFunc("GET /api/builds/{id}/logs", s.handleGetLogs)
	mux.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)
	mux.HandleFunc("DELETE /api/builds/{id}", s.handleDeleteBuild)

	mux.HandleFunc("PUT /api/projects/{id}", s.handleUpdateProject)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handleUpdateSettings)
	mux.HandleFunc("POST /api/projects/{id}/bump", s.handleProjectBump)
	mux.HandleFunc("GET /api/builds/{id}/tags", s.handleGetBuildTags)
	mux.HandleFunc("POST /api/builds/{id}/tags", s.handleAddBuildTag)
	mux.HandleFunc("DELETE /api/builds/{id}/tags/{tag}", s.handleDeleteBuildTag)
	mux.HandleFunc("POST /api/builds/{id}/push", s.handlePushBuild)

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
		INSERT INTO projects (name, repo_url, branch, image_name, enabled, registry_override) 
		VALUES (?, ?, ?, ?, ?, ?)`,
		p.Name, p.RepoURL, p.Branch, p.ImageName, true, p.RegistryOverride, // Auto-enable for MVP
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

	// 1. Fetch log path, version, and image name
	var logsPath, version, imageName string
	err := s.db.QueryRow(`
		SELECT b.logs_path, b.version, p.image_name 
		FROM builds b JOIN projects p ON b.project_id = p.id 
		WHERE b.id = ?`, buildID).Scan(&logsPath, &version, &imageName)

	if err == nil {
		delErr := deleteRegistryTag(s.cfg.RegistryURL, imageName, version)
		if delErr != nil {
			log.Printf("Note: Failed to delete build %s from registry (may already be deleted): %v", version, delErr)
		} else {
			// Trigger GC in the background to physically free up the hard drive
			go RunGarbageCollection(s.cfg.RegistryContainer)
		}

		// 3. Delete the physical log file
		if logsPath != "" {
			os.Remove(logsPath)
		}
	}

	s.db.Exec("DELETE FROM tags WHERE build_id = ?", buildID)
	s.db.Exec("DELETE FROM builds WHERE id = ?", buildID)

	BroadcastEvent("update") 
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	
	var payload struct {
		RepoURL    string `json:"repo_url"`
		Branch     string `json:"branch"`
		CustomTags string `json:"custom_tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	s.db.Exec("UPDATE projects SET repo_url = ?, branch = ?, custom_tags = ? WHERE id = ?", 
		payload.RepoURL, payload.Branch, payload.CustomTags, projectID)
	w.WriteHeader(http.StatusOK)
}

// --- Advanced Features Handlers ---

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings := LoadSettings(s.cfg.DataDir)
	
	if settings.GHToken != "" {
		settings.GHToken = "********" 
	}
	// Mask registry passwords
	for i := range settings.Registries {
		if settings.Registries[i].Password != "" {
			settings.Registries[i].Password = "********"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var payload GlobalSettings
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	current := LoadSettings(s.cfg.DataDir)
	
	if payload.GHToken == "********" || payload.GHToken == "" {
		payload.GHToken = current.GHToken
	}

	// Handle masking for registries and trigger Docker Login!
	for i, reg := range payload.Registries {
		if reg.Password == "********" || reg.Password == "" {
			// Find original password
			for _, oldReg := range current.Registries {
				if oldReg.URL == reg.URL {
					payload.Registries[i].Password = oldReg.Password
					break
				}
			}
		}
		// Actually authenticate the host Docker daemon!
		DockerLogin(payload.Registries[i])
	}

	if err := SaveSettings(s.cfg.DataDir, payload); err != nil {
		log.Printf("ERROR saving settings to disk: %v\n", err)
		http.Error(w, "Failed to write settings to disk", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleProjectBump(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	
	var payload struct {
		Type string `json:"type"` // "minor" or "major"
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if payload.Type != "minor" && payload.Type != "major" {
		http.Error(w, "Type must be minor or major", http.StatusBadRequest)
		return
	}

	s.db.Exec("UPDATE state SET next_bump = ? WHERE project_id = ?", payload.Type, projectID)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleAddBuildTag(w http.ResponseWriter, r *http.Request) {
	buildID := r.PathValue("id")
	
	var payload struct {
		Tag string `json:"tag"` // e.g. "stable"
	}
	json.NewDecoder(r.Body).Decode(&payload)

	// Fetch the exact image name and version of this specific build
	var version, imageName string
	err := s.db.QueryRow(`
		SELECT b.version, p.image_name 
		FROM builds b JOIN projects p ON b.project_id = p.id 
		WHERE b.id = ?`, buildID).Scan(&version, &imageName)
	
	if err != nil {
		http.Error(w, "Build not found", http.StatusNotFound)
		return
	}

	sourceImage := fmt.Sprintf("%s:%s", imageName, version)
	newImageTag := fmt.Sprintf("%s:%s", imageName, payload.Tag)

	// Execute remote imagetools tagging
	if err := TagExistingImage(sourceImage, newImageTag); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Log it in the DB so it shows up in the UI
	s.db.Exec("INSERT INTO tags (build_id, tag) VALUES (?, ?)", buildID, payload.Tag)

	w.WriteHeader(http.StatusOK)
}


func (s *Server) handlePushBuild(w http.ResponseWriter, r *http.Request) {
	buildID := r.PathValue("id")
	
	var payload struct {
		Registry string `json:"registry"`
	}
	json.NewDecoder(r.Body).Decode(&payload)

	if payload.Registry == "" {
		http.Error(w, "Target registry required", http.StatusBadRequest)
		return
	}

	var version, sourceImageName, projectName string
	err := s.db.QueryRow(`
		SELECT b.version, p.image_name, p.name 
		FROM builds b JOIN projects p ON b.project_id = p.id 
		WHERE b.id = ?`, buildID).Scan(&version, &sourceImageName, &projectName)
	
	if err != nil {
		http.Error(w, "Build not found", http.StatusNotFound)
		return
	}

	settings := LoadSettings(s.cfg.DataDir)
	var nameSpace string 
	for _, i := range settings.Registries{
		if i.URL == payload.Registry{
			nameSpace = i.Username
			break
		}
	}

	sourceImage := fmt.Sprintf("%s:%s", sourceImageName, version)
	targetImage := fmt.Sprintf("%s/%s/%s:%s", payload.Registry, nameSpace, projectName, version)

	// Instantly copy the manifest across registries!
	if err := TagExistingImage(sourceImage, targetImage); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetBuildTags(w http.ResponseWriter, r *http.Request) {
	buildID := r.PathValue("id")
	
	rows, err := s.db.Query("SELECT tag FROM tags WHERE build_id = ?", buildID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			tags = append(tags, t)
		}
	}
	if tags == nil {
		tags = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}

func (s *Server) handleDeleteBuildTag(w http.ResponseWriter, r *http.Request) {
	buildID := r.PathValue("id")
	tag := r.PathValue("tag")

	var imageName string
	err := s.db.QueryRow("SELECT p.image_name FROM projects p JOIN builds b ON p.id = b.project_id WHERE b.id = ?", buildID).Scan(&imageName)
	if err != nil || imageName == "" {
		http.Error(w, "Build not found", http.StatusNotFound)
		return
	}

	// --- 1. THE DUMMY IMAGE HACK ---
	// Create a temporary directory with an empty Dockerfile
	tmpDir, _ := os.MkdirTemp("", "shipper-dummy-*")
	defer os.RemoveAll(tmpDir)
	os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM scratch\nLABEL maintainer=\"shipper-cleanup\""), 0644)

	dummyImageTag := fmt.Sprintf("%s:%s", imageName, tag)

	// Build and push the empty image to steal the tag pointer away from the real build
	cmd := exec.Command("docker", "buildx", "build", "--push", "-t", dummyImageTag, ".")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to push dummy image for untagging: %v", err)
	}

	// --- 2. DELETE THE DUMMY DIGEST ---
	// deleteRegistryTag will now target the useless dummy image, leaving your real build safe!
	_ = deleteRegistryTag(s.cfg.RegistryURL, imageName, tag)

	// 3. Delete from our SQLite Database
	s.db.Exec("DELETE FROM tags WHERE build_id = ? AND tag = ?", buildID, tag)
	
	// Tell the UI to refresh
	BroadcastEvent("update")
	w.WriteHeader(http.StatusOK)
}