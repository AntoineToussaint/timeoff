/*
main.go - Application entry point

PURPOSE:
  Initializes and starts the Warp Resource Management Engine server.
  Handles configuration, dependency injection, and graceful shutdown.

STARTUP SEQUENCE:
  1. Parse command-line flags
  2. Initialize SQLite store
  3. Create API handler with dependencies
  4. Configure HTTP router
  5. Start server with graceful shutdown

COMMAND-LINE FLAGS:
  -port    HTTP server port (default: 8080)
  -db      SQLite database path (default: timeoff.db)
           Use ":memory:" for in-memory database

GRACEFUL SHUTDOWN:
  On SIGINT/SIGTERM:
  1. Stop accepting new connections
  2. Wait for active requests to complete (30s timeout)
  3. Close database connection
  4. Exit

EXAMPLES:
  # Run with file database
  ./server -db="./data/warp.db"

  # Run with in-memory database
  ./server -db=":memory:"

  # Run on different port
  ./server -port=3000

ENVIRONMENT:
  No environment variables currently. All config via flags.
  Future: DATABASE_URL, PORT, LOG_LEVEL

SEE ALSO:
  - api/server.go: Router configuration
  - api/handlers.go: HTTP handlers
  - store/sqlite/sqlite.go: Database implementation
*/
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/warp/resource-engine/api"
	"github.com/warp/resource-engine/store/sqlite"
)

func main() {
	// Flags
	port := flag.Int("port", 8080, "HTTP server port")
	dbPath := flag.String("db", "timeoff.db", "SQLite database path")
	flag.Parse()

	// Initialize store
	store, err := sqlite.New(*dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer store.Close()

	// Initialize handler
	handler := api.NewHandler(store)
	
	// Load existing policies into cache
	if err := handler.LoadPolicies(context.Background()); err != nil {
		log.Printf("Warning: Failed to load policies: %v", err)
	}

	// Create router
	router := api.NewRouter(handler)

	// Create server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("🚀 Server starting on http://localhost:%d", *port)
		log.Printf("📊 API available at http://localhost:%d/api", *port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}
