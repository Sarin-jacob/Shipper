// backend/internal/events.go
package internal

import (
	"fmt"
	"net/http"
	"sync"
)

var (
	// clients holds all open browser connections
	clients   = make(map[chan string]bool)
	clientsMu sync.Mutex
)

// BroadcastEvent sends a message to all connected browsers
func BroadcastEvent(message string) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	for client := range clients {
		client <- message
	}
}

// HandleSSE keeps a connection open to the browser and pushes events
func HandleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientChan := make(chan string)
	
	clientsMu.Lock()
	clients[clientChan] = true
	clientsMu.Unlock()

	// Clean up when the browser closes the tab
	defer func() {
		clientsMu.Lock()
		delete(clients, clientChan)
		clientsMu.Unlock()
		close(clientChan)
	}()

	for {
		select {
		case msg := <-clientChan:
			// Push the event to the browser
			fmt.Fprintf(w, "data: %s\n\n", msg)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}