package migration

import (
	"context"
	"encoding/json"
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

type createMigrationRequest struct {
	SourcePod  string `json:"source_pod"`
	Namespace  string `json:"namespace"`
	TargetNode string `json:"target_node"`
}

type createMigrationResponse struct {
	MigrationID string `json:"migration_id"`
	Status      string `json:"status"`
}

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
			http.Error(w, fmt.Sprintf("failed to initiate migration: %v", err), http.StatusInternalServerError)
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

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"healthy","service":"migration-manager"}`))
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(s.manager.metrics.RenderPrometheus()))
}
