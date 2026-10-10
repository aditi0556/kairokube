package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Server provides the HTTP API for initiating and monitoring migrations.
type Server struct {
	manager *Manager
	mux     *http.ServeMux
	server  *http.Server
}

// NewServer initializes the HTTP API server with all migration routes.
func NewServer(mgr *Manager, addr string) *Server {
	mux := http.NewServeMux()
	s := &Server{
		manager: mgr,
		mux:     mux,
		server: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
		},
	}
	s.registerRoutes()
	return s
}

// registerRoutes binds every HTTP path served by the manager to its handler.
// Paths are registered once in NewServer; the mux is not modified afterwards.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/migrations", s.handleMigrations)
	s.mux.HandleFunc("/migrations/", s.handleMigrationByID)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/metrics", s.handleMetrics)
}

// Start begins listening on the configured address.
func (s *Server) Start() error {
	log.Printf("Migration Manager API listening on %s", s.server.Addr)
	return s.server.ListenAndServe()
}

// Stop gracefully terminates the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// createMigrationRequest is the JSON body accepted by POST /migrations.
// SourcePod and TargetNode are required; Namespace defaults to "default".
type createMigrationRequest struct {
	SourcePod  string `json:"source_pod"`
	Namespace  string `json:"namespace"`
	TargetNode string `json:"target_node"`
}

// createMigrationResponse is returned with HTTP 202 when a migration is accepted.
// MigrationID identifies the record for later GET /migrations/{id} calls.
type createMigrationResponse struct {
	MigrationID string `json:"migration_id"`
	Status      string `json:"status"`
}

// handleMigrations serves /migrations. POST validates the request and starts a
// migration asynchronously (202, or 400/409 on bad input or conflict); GET lists
// every known migration snapshot. Other methods receive 405.
func (s *Server) handleMigrations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req createMigrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
			return
		}
		if req.SourcePod == "" || req.TargetNode == "" {
			http.Error(w, "source_pod and target_node are required", http.StatusBadRequest)
			return
		}
		if req.Namespace == "" {
			req.Namespace = "default"
		}

		mig, err := s.manager.StartMigrationAsync(req.SourcePod, req.Namespace, req.TargetNode)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, ErrInvalidMigrationRequest):
				status = http.StatusBadRequest
			case errors.Is(err, ErrMigrationConflict):
				status = http.StatusConflict
			}
			http.Error(w, fmt.Sprintf("failed to initiate migration: %v", err), status)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(createMigrationResponse{
			MigrationID: mig.ID,
			Status:      string(mig.GetState()),
		})

	case http.MethodGet:
		migrations := s.manager.ListMigrations()
		snapshots := make([]MigrationSnapshot, len(migrations))
		for i, m := range migrations {
			snapshots[i] = m.Snapshot()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshots)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMigrationByID serves GET /migrations/{id}. It returns the migration
// snapshot as JSON, 404 if the ID is unknown, and 400 if the ID is empty.
func (s *Server) handleMigrationByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/migrations/")
	if id == "" {
		http.Error(w, "migration ID is required", http.StatusBadRequest)
		return
	}

	mig, ok := s.manager.GetMigration(id)
	if !ok {
		http.Error(w, fmt.Sprintf("migration %q not found", id), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(mig.Snapshot())
}

// handleHealth serves GET /health with a static healthy status. It reports that
// the HTTP process is up; it does not check Kubernetes or RabbitMQ connectivity.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"healthy","service":"migration-manager"}`))
}

// handleMetrics serves GET /metrics in Prometheus text exposition format, built
// from the manager's MetricsCollector.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(s.manager.metrics.RenderPrometheus()))
}
