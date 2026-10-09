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
