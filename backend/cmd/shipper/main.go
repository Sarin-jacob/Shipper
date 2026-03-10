// backend/cmd/shiper/main.go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/Sarin-jacob/Shipper/internal"
	"github.com/Sarin-jacob/Shipper/internal"
)

func main() {
	log.Println("Starting Shiper...")

	os.MkdirAll("./data", os.ModePerm)

	database, err := internal.InitDB("./data/shipper.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Initialize API Server
	server := internal.NewServer(database)
	mux := server.SetupRoutes()

	log.Println("API Server running on http://localhost")
	if err := http.ListenAndServe(":80", mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}