/*
server.go - HTTP router and middleware configuration

PURPOSE:
  Configures the HTTP router (chi), middleware stack, and route definitions.
  This is the wiring layer that connects URLs to handlers.

ROUTER: chi
  Chi was chosen for:
  - Lightweight and fast
  - Context-based
  - Middleware support
  - RESTful route patterns

MIDDLEWARE STACK:
  1. Logger:     Request logging
  2. Recoverer:  Panic recovery (500 instead of crash)
  3. RequestID:  Unique ID per request for tracing
  4. CORS:       Cross-origin requests for frontend

ROUTE GROUPS:
  /api/employees/*      Employee management
  /api/policies/*       Policy management
  /api/scenarios/*      Demo scenarios
  /api/admin/*          Admin operations
  /api/reset            Database reset (dev only)
  /*                    Static files (frontend)

STATIC FILE SERVING:
  In production, serves the built React app from web/dist/.
  Falls back to index.html for client-side routing.

SECURITY NOTE:
  No authentication middleware currently. All endpoints are public.
  See DEVOPS_SECURITY.md for production requirements.

SEE ALSO:
  - handlers.go: Handler implementations
  - cmd/server/main.go: Server startup
*/
package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// NewRouter creates a new router with all routes configured.
func NewRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "http://localhost:8080"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Employee routes
		r.Route("/employees", func(r chi.Router) {
			r.Get("/", h.ListEmployees)
			r.Post("/", h.CreateEmployee)
			r.Get("/{id}", h.GetEmployee)
			r.Get("/{id}/balance", h.GetBalance)
			r.Get("/{id}/transactions", h.GetTransactions)
			r.Get("/{id}/assignments", h.GetAssignments)
			r.Post("/{id}/requests", h.SubmitRequest)
		})

		// Transaction routes
		r.Route("/transactions", func(r chi.Router) {
			r.Delete("/{id}", h.CancelTransaction)
		})

		// Policy routes
		r.Route("/policies", func(r chi.Router) {
			r.Get("/", h.ListPolicies)
			r.Post("/", h.CreatePolicy)
			r.Get("/{id}", h.GetPolicy)
		})

		// Admin routes
		r.Route("/admin", func(r chi.Router) {
			r.Post("/assignments", h.CreateAssignment)
			r.Post("/rollover", h.TriggerRollover)
			r.Post("/adjustments", h.CreateAdjustment)
		})

		// Holiday routes
		r.Route("/holidays", func(r chi.Router) {
			r.Get("/", h.ListHolidays)
			r.Post("/", h.CreateHoliday)
			r.Post("/defaults", h.AddDefaultHolidays)
			r.Delete("/{id}", h.DeleteHoliday)
		})

		// Request approval routes
		r.Route("/requests", func(r chi.Router) {
			r.Get("/pending", h.ListPendingRequests)
			r.Post("/{id}/approve", h.ApproveRequest)
			r.Post("/{id}/reject", h.RejectRequest)
		})

		// Reconciliation routes
		r.Route("/reconciliation", func(r chi.Router) {
			r.Get("/runs", h.ListReconciliationRuns)
			r.Post("/process", h.TriggerRollover) // Existing endpoint
		})

		// Scenario routes
		r.Route("/scenarios", func(r chi.Router) {
			r.Get("/", h.ListScenarios)
			r.Get("/current", h.GetCurrentScenario)
			r.Post("/load", h.LoadScenario)
			r.Post("/reset", h.ResetDatabase)
		})
	})

	// Serve static files (React app)
	// First try ./web/dist (development), then fall back to message
	staticDir := "./web/dist"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		// Try relative to executable
		exe, _ := os.Executable()
		staticDir = filepath.Join(filepath.Dir(exe), "web", "dist")
	}

	if _, err := os.Stat(staticDir); err == nil {
		fileServer := http.FileServer(http.Dir(staticDir))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			fullPath := filepath.Join(staticDir, path)
			
			// Check if file exists
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				// SPA routing: serve index.html
				http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	} else {
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Time Resource Engine</title></head>
<body style="font-family: system-ui; max-width: 800px; margin: 50px auto; padding: 20px;">
<h1>Time Resource Engine API</h1>
<p>The frontend is not built yet. Run <code>cd web && npm install && npm run build</code></p>
<h2>API Endpoints</h2>
<ul>
<li><a href="/api/employees">/api/employees</a> - List employees</li>
<li><a href="/api/policies">/api/policies</a> - List policies</li>
<li><a href="/api/scenarios">/api/scenarios</a> - List scenarios</li>
</ul>
</body>
</html>`))
		})
	}

	return r
}
