package migration

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aditi0556/kairokube/pkg/checkpoint"
	"github.com/aditi0556/kairokube/pkg/config"
	"github.com/aditi0556/kairokube/pkg/transfer"
)

func TestEndToEndMigrationSuccess(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "kairokube-test-success")
	_ = os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		MaxReplayTime:     1 * time.Second,
		MinCutoffTime:     100 * time.Millisecond,
		MaxCutoffTime:     2 * time.Second,
		MetricsWindow:     1 * time.Second,
		CheckpointTimeout: 5 * time.Second,
		TransferTimeout:   5 * time.Second,
		RestoreTimeout:    5 * time.Second,
		MigrationTimeout:  10 * time.Second,
		FeasibilityPolicy: "warn",
		MigrationMode:     "mock",
		CheckpointDir:     tempDir,
	}

	cp := checkpoint.NewMockCheckpointProvider(tempDir)
	tp := &transfer.MockTransferProvider{}
	wc := NewMockWorkloadController()
	metrics := NewMetricsCollector()

	mgr := NewManager(cfg, nil, nil, cp, tp, wc, metrics)

	mig, err := mgr.StartMigrationAsync("consumer-0", "default", "worker-2")
	if err != nil {
		t.Fatalf("failed to start migration async: %v", err)
	}

	// Poll until COMPLETED or FAILED
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := mig.GetState()
		if st == StateCompleted {
			break
		}
		if st == StateFailed {
			t.Fatalf("migration failed unexpectedly: %s", mig.Snapshot().ErrorReason)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if mig.GetState() != StateCompleted {
		t.Fatalf("expected state COMPLETED, got %s", mig.GetState())
	}

	if got := atomic.LoadInt64(&metrics.MigrationSuccessTotal); got != 1 {
		t.Errorf("expected MigrationSuccessTotal=1, got %d", got)
	}
}

func TestCheckpointFailureTriggersRollback(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "kairokube-test-failure")
	_ = os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		MaxReplayTime:     1 * time.Second,
		MinCutoffTime:     100 * time.Millisecond,
		MaxCutoffTime:     2 * time.Second,
		MetricsWindow:     1 * time.Second,
		CheckpointTimeout: 5 * time.Second,
		TransferTimeout:   5 * time.Second,
		RestoreTimeout:    5 * time.Second,
		MigrationTimeout:  10 * time.Second,
		FeasibilityPolicy: "warn",
		MigrationMode:     "mock",
		CheckpointDir:     tempDir,
	}

	cp := checkpoint.NewMockCheckpointProvider(tempDir)
	cp.SimulateFailure = true
	cp.FailureMessage = "Kubelet FCC daemon error"

	tp := &transfer.MockTransferProvider{}
	wc := NewMockWorkloadController()
	metrics := NewMetricsCollector()

	mgr := NewManager(cfg, nil, nil, cp, tp, wc, metrics)

	mig, err := mgr.StartMigrationAsync("consumer-0", "default", "worker-2")
	if err != nil {
		t.Fatalf("failed to start migration: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := mig.GetState()
		if st == StateFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if mig.GetState() != StateFailed {
		t.Fatalf("expected state FAILED after rollback, got %s", mig.GetState())
	}

	if got := atomic.LoadInt64(&metrics.MigrationFailureTotal); got != 1 {
		t.Errorf("expected MigrationFailureTotal=1, got %d", got)
	}
}

func TestWorkloadSourceValidationFailure(t *testing.T) {
	cfg := &config.Config{
		MigrationTimeout:  5 * time.Second,
		FeasibilityPolicy: "warn",
	}

	cp := checkpoint.NewMockCheckpointProvider("")
	tp := &transfer.MockTransferProvider{}
	wc := NewMockWorkloadController()
	wc.SourceHealthy = false // simulate dead source pod

	metrics := NewMetricsCollector()
	mgr := NewManager(cfg, nil, nil, cp, tp, wc, metrics)

	mig, err := mgr.StartMigrationAsync("dead-pod", "default", "worker-2")
	if err != nil {
		t.Fatalf("failed to start migration: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mig.GetState() == StateFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if mig.GetState() != StateFailed {
		t.Fatalf("expected FAILED on source validation failure, got %s", mig.GetState())
	}
}

func TestManagerRejectsConcurrentMigrationForSameSource(t *testing.T) {
	mgr := NewManager(&config.Config{MigrationMode: "mock"}, nil, nil,
		checkpoint.NewMockCheckpointProvider(""), &transfer.MockTransferProvider{},
		NewMockWorkloadController(), NewMetricsCollector())
	if _, err := mgr.CreateMigration("consumer-0", "default", "worker-1"); err != nil {
		t.Fatalf("first migration was rejected: %v", err)
	}
	if _, err := mgr.CreateMigration("consumer-0", "default", "worker-2"); err == nil {
		t.Fatal("expected concurrent migration for the same source Pod to be rejected")
	}
}
