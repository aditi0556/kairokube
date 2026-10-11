package checkpoint

import (
	"context"
	"errors"
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
	if !errors.Is(err, ErrNodeRestoreUnavailable) {
		t.Fatalf("expected ErrNodeRestoreUnavailable, got %v", err)
	}
}

type fakeRestorer struct {
	calls int
	err   error
}

func (f *fakeRestorer) RestoreArchive(ctx context.Context, archivePath, containerID string) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return containerID + "-restored", nil
}

func restoreFixture(t *testing.T) (*CheckpointResult, string) {
	t.Helper()
	dir := t.TempDir()
	archive := filepath.Join(dir, "checkpoint.tar")
	criu := filepath.Join(dir, "criu")
	if err := os.WriteFile(archive, []byte("archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(criu, []byte("bin"), 0755); err != nil {
		t.Fatal(err)
	}
	return &CheckpointResult{FilePath: archive}, criu
}

func TestKubeletRestoreSucceedsThroughRestorer(t *testing.T) {
	ck, criu := restoreFixture(t)
	fr := &fakeRestorer{}
	p := NewKubeletFCCProvider(nil, nil, KubeletFCCConfig{}).WithRestorer(fr, criu)
	if err := p.RestoreCheckpoint(context.Background(), ck, Target{Namespace: "default", PodName: "counter", NodeName: "worker-2"}); err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if fr.calls != 1 {
		t.Fatalf("restorer calls = %d, want 1", fr.calls)
	}
}

func TestKubeletRestoreReportsRestorerFailure(t *testing.T) {
	ck, criu := restoreFixture(t)
	p := NewKubeletFCCProvider(nil, nil, KubeletFCCConfig{}).WithRestorer(&fakeRestorer{err: errors.New("boom")}, criu)
	if err := p.RestoreCheckpoint(context.Background(), ck, Target{PodName: "c", NodeName: "n"}); err == nil {
		t.Fatal("restorer failure must be returned, not swallowed")
	}
}

func TestKubeletRestorePreflightStopsBeforeRestorer(t *testing.T) {
	ck, _ := restoreFixture(t)
	fr := &fakeRestorer{}
	p := NewKubeletFCCProvider(nil, nil, KubeletFCCConfig{}).WithRestorer(fr, "/does/not/exist/criu")
	if err := p.RestoreCheckpoint(context.Background(), ck, Target{PodName: "c", NodeName: "n"}); err == nil {
		t.Fatal("missing CRIU must fail preflight")
	}
	if fr.calls != 0 {
		t.Fatal("restorer must not run when preflight fails")
	}
}
