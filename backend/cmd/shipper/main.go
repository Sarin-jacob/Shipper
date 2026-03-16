// backend/cmd/shiper/main.go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/Sarin-jacob/Shipper/internal"
)

func main() {
	log.Println("Starting Shiper CI/CD Engine...")

	// 1. Load configuration
	cfg := internal.LoadConfig()

	// 2. Ensure data directories exist
	os.MkdirAll(cfg.DataDir, os.ModePerm)
	os.MkdirAll(cfg.DataDir+"/logs", os.ModePerm)

	// 3. Initialize Database
	db, err := internal.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// 4. Authenticate external registries
	internal.AuthenticateAllRegistries(cfg.DataDir)

	// 5. Start Background Scheduler
	scheduler := internal.NewScheduler(db, cfg)
	scheduler.Start()

	// 6. Initialize API Server
	server := internal.NewServer(db, cfg)
	mux := server.SetupRoutes()

	// 7. Start listening
	log.Printf("API Server running on port %s (Registry: %s)\n", cfg.Port, cfg.RegistryURL)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}