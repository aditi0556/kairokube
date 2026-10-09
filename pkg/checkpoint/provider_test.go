package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMockCheckpointCreationAndRestoration(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "kairokube-ckpt-test")
	_ = os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	provider := NewMockCheckpointProvider(tempDir)
	ctx := context.Background()

	res, err := provider.CreateCheckpoint(ctx, "default", "consumer-0", "consumer")
	if err != nil {
		t.Fatalf("failed to create checkpoint: %v", err)
	}

	if res.FilePath == "" {
		t.Fatal("expected non-empty FilePath")
	}
	if res.SizeBytes <= 0 {
		t.Fatalf("expected positive SizeBytes, got %d", res.SizeBytes)
	}
	if res.Checksum == "" {
		t.Fatal("expected non-empty SHA256 checksum")
	}

	// Verify file actually exists on disk
	fi, err := os.Stat(res.FilePath)
	if err != nil {
		t.Fatalf("checkpoint file not found on disk: %v", err)
	}
	if fi.Size() != res.SizeBytes {
		t.Errorf("size mismatch: %d on disk vs %d in result", fi.Size(), res.SizeBytes)
	}

	// Verify restoration
	target := Target{
		Namespace: "default",
		PodName:   "consumer-0-target",
		NodeName:  "worker-2",
	}
	if err := provider.RestoreCheckpoint(ctx, res, target); err != nil {
		t.Fatalf("failed to restore checkpoint: %v", err)
	}
}

func TestMockCheckpointFailureSimulation(t *testing.T) {
	provider := NewMockCheckpointProvider("")
	provider.SimulateFailure = true
	provider.FailureMessage = "Disk full"

	ctx := context.Background()
	_, err := provider.CreateCheckpoint(ctx, "default", "consumer-0", "consumer")
	if err == nil {
		t.Fatal("expected simulated failure, got nil")
	}
}

func TestKubeletRestoreFailsWhenRuntimeRestoreIsUnavailable(t *testing.T) {
	provider := NewKubeletFCCProvider(nil, nil, KubeletFCCConfig{})
	err := provider.RestoreCheckpoint(context.Background(), &CheckpointResult{FilePath: "checkpoint.tar"}, Target{Namespace: "default", PodName: "target", NodeName: "worker-2"})
	if err == nil {
		t.Fatal("expected explicit unsupported restore error")
	}
}
