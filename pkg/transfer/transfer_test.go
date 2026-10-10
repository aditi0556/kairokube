package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestFileTransferProviderSuccess(t *testing.T) {
	srcDir := filepath.Join(os.TempDir(), "kairokube-src")
	destDir := filepath.Join(os.TempDir(), "kairokube-dest")
	_ = os.MkdirAll(srcDir, 0755)
	_ = os.MkdirAll(destDir, 0755)
	defer os.RemoveAll(srcDir)
	defer os.RemoveAll(destDir)

	srcFile := filepath.Join(srcDir, "artifact.tar")
	content := []byte("MS2M checkpoint test payload data")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	hasher := sha256.New()
	hasher.Write(content)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	provider := NewFileTransferProvider(destDir)
	ctx := context.Background()

	res, err := provider.Transfer(ctx, srcFile, "worker-2", destDir, checksum)
	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	if !res.Success {
		t.Fatal("expected transfer result Success=true")
	}
	if res.SizeBytes != int64(len(content)) {
		t.Errorf("size mismatch: %d vs %d", res.SizeBytes, len(content))
	}

	// Verify destination file
	destData, err := os.ReadFile(res.DestinationPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(destData) != string(content) {
		t.Fatal("content mismatch in transferred file")
	}
}

func TestFileTransferChecksumMismatch(t *testing.T) {
	srcDir := filepath.Join(os.TempDir(), "kairokube-src-mismatch")
	destDir := filepath.Join(os.TempDir(), "kairokube-dest-mismatch")
	_ = os.MkdirAll(srcDir, 0755)
	_ = os.MkdirAll(destDir, 0755)
	defer os.RemoveAll(srcDir)
	defer os.RemoveAll(destDir)

	srcFile := filepath.Join(srcDir, "artifact.tar")
	_ = os.WriteFile(srcFile, []byte("valid content"), 0644)

	provider := NewFileTransferProvider(destDir)
	ctx := context.Background()

	_, err := provider.Transfer(ctx, srcFile, "worker-2", destDir, "wrongchecksum123")
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
}

// TestFileTransferSameDirectoryDoesNotTruncateSource guards against a data-loss
// bug where the destination path equals the source path and os.Create
// truncated the checkpoint before it could be read.
func TestFileTransferSameDirectoryDoesNotTruncateSource(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "kairokube-same-dir")
	_ = os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	srcFile := filepath.Join(dir, "artifact.tar")
	content := []byte("MS2M checkpoint same-directory payload")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])

	provider := NewFileTransferProvider(dir)
	res, err := provider.Transfer(context.Background(), srcFile, "worker-2", dir, checksum)
	if err == nil {
		t.Fatal("expected same-file transfer to be refused")
	}
	if res == nil || res.Success {
		t.Fatalf("expected unsuccessful result for same-file transfer, got %+v", res)
	}

	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("failed to read artifact after transfer: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("artifact was modified by same-directory transfer: got %d bytes, want %d", len(data), len(content))
	}
}

