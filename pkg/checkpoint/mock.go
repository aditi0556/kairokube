package checkpoint

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// MockCheckpointProvider provides a fully functional local checkpoint implementation for testing,
// CI/CD pipelines, and environments where Kubelet FCC is not available.
// It creates real tar archives containing simulated process state.
type MockCheckpointProvider struct {
	BaseDir           string
	SimulatedDuration time.Duration
	SimulateFailure   bool
	FailureMessage    string
}

// NewMockCheckpointProvider creates a MockCheckpointProvider.
func NewMockCheckpointProvider(baseDir string) *MockCheckpointProvider {
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "kairokube-checkpoints")
	}
	_ = os.MkdirAll(baseDir, 0755)
	return &MockCheckpointProvider{
		BaseDir:           baseDir,
		SimulatedDuration: 50 * time.Millisecond,
	}
}

// CreateCheckpoint creates a real tar archive containing simulated memory state and computes its checksum.
func (m *MockCheckpointProvider) CreateCheckpoint(ctx context.Context, namespace, podName, containerName string) (*CheckpointResult, error) {
	if m.SimulateFailure {
		msg := m.FailureMessage
		if msg == "" {
			msg = "simulated checkpoint creation failure"
		}
		return nil, fmt.Errorf("%s", msg)
	}

	start := time.Now()
	if m.SimulatedDuration > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.SimulatedDuration):
		}
	}

	filename := fmt.Sprintf("checkpoint-%s_%s-%s-%d.tar", podName, namespace, containerName, time.Now().UnixNano())
	filePath := filepath.Join(m.BaseDir, filename)

	f, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create checkpoint file %s: %w", filePath, err)
	}
	defer f.Close()

	hasher := sha256.New()
	multiWriter := io.MultiWriter(f, hasher)
	tw := tar.NewWriter(multiWriter)

	// Write mock CRIU dump metadata and memory page
	stateContent := []byte(fmt.Sprintf("MS2M checkpoint state for %s/%s/%s at %s", namespace, podName, containerName, time.Now().Format(time.RFC3339)))
	hdr := &tar.Header{
		Name:    "descriptors.json",
		Mode:    0600,
		Size:    int64(len(stateContent)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, fmt.Errorf("failed to write tar header: %w", err)
	}
	if _, err := tw.Write(stateContent); err != nil {
		return nil, fmt.Errorf("failed to write tar body: %w", err)
	}

	// Add dummy pages.1 (100KB to represent memory chunk)
	dummyPages := make([]byte, 100*1024)
	pageHdr := &tar.Header{
		Name:    "pages.1.img",
		Mode:    0600,
		Size:    int64(len(dummyPages)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(pageHdr); err != nil {
		return nil, fmt.Errorf("failed to write tar page header: %w", err)
	}
	if _, err := tw.Write(dummyPages); err != nil {
		return nil, fmt.Errorf("failed to write tar page body: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize tar writer: %w", err)
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat checkpoint file: %w", err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	return &CheckpointResult{
		Namespace:     namespace,
		PodName:       podName,
		ContainerName: containerName,
		NodeName:      "source-node",
		FilePath:      filePath,
		SizeBytes:     fi.Size(),
		CreatedAt:     time.Now().UTC(),
		Duration:      time.Since(start),
		Checksum:      checksum,
	}, nil
}

// RestoreCheckpoint simulates validating and restoring the checkpoint archive.
func (m *MockCheckpointProvider) RestoreCheckpoint(ctx context.Context, checkpoint *CheckpointResult, target Target) error {
	return m.ValidateRestore(ctx, checkpoint, target)
}

// ValidateRestore verifies mock artifact and target metadata without restoring process memory.
func (m *MockCheckpointProvider) ValidateRestore(ctx context.Context, checkpoint *CheckpointResult, target Target) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.SimulateFailure {
		return fmt.Errorf("simulated restore failure")
	}
	if checkpoint == nil {
		return fmt.Errorf("checkpoint cannot be nil")
	}
	if _, err := os.Stat(checkpoint.FilePath); err != nil {
		return fmt.Errorf("checkpoint artifact not found at %s: %w", checkpoint.FilePath, err)
	}
	if target.NodeName == "" {
		return fmt.Errorf("target node name must not be empty")
	}
	return nil
}

var _ CheckpointProvider = (*MockCheckpointProvider)(nil)
