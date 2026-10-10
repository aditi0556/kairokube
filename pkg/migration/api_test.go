package migration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aditi0556/kairokube/pkg/checkpoint"
	"github.com/aditi0556/kairokube/pkg/config"
	"github.com/aditi0556/kairokube/pkg/transfer"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	mgr := NewManager(&config.Config{
		MigrationMode:     "mock",
		FeasibilityPolicy: "warn",
		MaxReplayTime:     1 * time.Second,
		MinCutoffTime:     100 * time.Millisecond,
		MaxCutoffTime:     2 * time.Second,
		MetricsWindow:     1 * time.Second,
		CheckpointTimeout: 5 * time.Second,
		TransferTimeout:   5 * time.Second,
		RestoreTimeout:    5 * time.Second,
		MigrationTimeout:  10 * time.Second,
	}, nil, nil,
		checkpoint.NewMockCheckpointProvider(t.TempDir()), &transfer.MockTransferProvider{},
		NewMockWorkloadController(), NewMetricsCollector())
	return NewServer(mgr, ":0")
}

func TestCreateMigrationStatusCodes(t *testing.T) {
	srv := newTestServer(t)

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/migrations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.handleMigrations(rec, req)
		return rec.Code
	}

	if code := post(`{"source_pod":"","target_node":"worker-2"}`); code != http.StatusBadRequest {
		t.Errorf("missing source_pod: want 400, got %d", code)
	}
	if code := post(`{"source_pod":"consumer-0","target_node":"worker-1"}`); code != http.StatusAccepted {
		t.Fatalf("first migration: want 202, got %d", code)
	}
	if code := post(`{"source_pod":"consumer-0","target_node":"worker-2"}`); code != http.StatusConflict {
		t.Errorf("duplicate active source: want 409, got %d", code)
	}
}
