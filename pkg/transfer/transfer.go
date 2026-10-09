// Package transfer provides checkpoint transfer orchestration across cluster nodes.
package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// TransferResult records full telemetry and artifact details for a transfer operation.
type TransferResult struct {
	SourcePath      string        `json:"source_path"`
	DestinationPath string        `json:"destination_path"`
	TargetNode      string        `json:"target_node"`
	SizeBytes       int64         `json:"size_bytes"`
	StartTime       time.Time     `json:"start_time"`
	EndTime         time.Time     `json:"end_time"`
	Duration        time.Duration `json:"duration"`
	Success         bool          `json:"success"`
	Checksum        string        `json:"checksum"`
	Error           string        `json:"error,omitempty"`
}

// TransferProvider defines the contract for transferring checkpoint artifacts between nodes.
type TransferProvider interface {
	Transfer(ctx context.Context, sourcePath, targetNode, destDir, expectedChecksum string) (*TransferResult, error)
}

// FileTransferProvider implements checkpoint transfer using file copying (for shared storage, hostPath, or local testing).
type FileTransferProvider struct {
	DefaultDestDir string
}

// NewFileTransferProvider creates a new FileTransferProvider.
func NewFileTransferProvider(defaultDestDir string) *FileTransferProvider {
	if defaultDestDir == "" {
		defaultDestDir = os.TempDir()
	}
	return &FileTransferProvider{
		DefaultDestDir: defaultDestDir,
	}
}

// Transfer copies the source artifact to the destination, computing and verifying the checksum.
func (p *FileTransferProvider) Transfer(ctx context.Context, sourcePath, targetNode, destDir, expectedChecksum string) (*TransferResult, error) {
	startTime := time.Now()

	// 1. Validate source checkpoint exists
	srcInfo, err := os.Stat(sourcePath)
	if err != nil {
		res := &TransferResult{
			SourcePath: sourcePath,
			TargetNode: targetNode,
			StartTime:  startTime,
			EndTime:    time.Now(),
			Success:    false,
			Error:      fmt.Sprintf("source checkpoint does not exist: %v", err),
		}
		return res, fmt.Errorf("source checkpoint not found at %s: %w", sourcePath, err)
	}

	if destDir == "" {
		destDir = p.DefaultDestDir
	}
	_ = os.MkdirAll(destDir, 0755)

	destPath := filepath.Join(destDir, filepath.Base(sourcePath))

	srcFile, err := os.Open(sourcePath)
	if err != nil {
		res := &TransferResult{
			SourcePath: sourcePath,
			TargetNode: targetNode,
			StartTime:  startTime,
			EndTime:    time.Now(),
			Success:    false,
			Error:      fmt.Sprintf("failed to open source file: %v", err),
		}
		return res, fmt.Errorf("failed to open source checkpoint %s: %w", sourcePath, err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		res := &TransferResult{
			SourcePath:      sourcePath,
			DestinationPath: destPath,
			TargetNode:      targetNode,
			StartTime:       startTime,
			EndTime:         time.Now(),
			Success:         false,
			Error:           fmt.Sprintf("failed to create destination file: %v", err),
		}
		return res, fmt.Errorf("failed to create destination file %s: %w", destPath, err)
	}
	defer destFile.Close()

	hasher := sha256.New()
	mw := io.MultiWriter(destFile, hasher)

	// Copy with context cancellation support via buffer
	buf := make([]byte, 32*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			res := &TransferResult{
				SourcePath:      sourcePath,
				DestinationPath: destPath,
				TargetNode:      targetNode,
				StartTime:       startTime,
				EndTime:         time.Now(),
				Success:         false,
				Error:           ctx.Err().Error(),
			}
			return res, ctx.Err()
		default:
		}

		nr, er := srcFile.Read(buf)
		if nr > 0 {
			nw, ew := mw.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = fmt.Errorf("invalid write result")
				}
			}
			written += int64(nw)
			if ew != nil {
				res := &TransferResult{
					SourcePath:      sourcePath,
					DestinationPath: destPath,
					TargetNode:      targetNode,
					StartTime:       startTime,
					EndTime:         time.Now(),
					Success:         false,
					Error:           fmt.Sprintf("failed writing to destination: %v", ew),
				}
				return res, fmt.Errorf("write error: %w", ew)
			}
		}
		if er != nil {
			if er != io.EOF {
				res := &TransferResult{
					SourcePath:      sourcePath,
					DestinationPath: destPath,
					TargetNode:      targetNode,
					StartTime:       startTime,
					EndTime:         time.Now(),
					Success:         false,
					Error:           fmt.Sprintf("read error: %v", er),
				}
				return res, fmt.Errorf("read error: %w", er)
			}
			break
		}
	}

	endTime := time.Now()
	duration := endTime.Sub(startTime)
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))

	// Verify checksum if provided
	if expectedChecksum != "" && actualChecksum != expectedChecksum {
		res := &TransferResult{
			SourcePath:      sourcePath,
			DestinationPath: destPath,
			TargetNode:      targetNode,
			SizeBytes:       written,
			StartTime:       startTime,
			EndTime:         endTime,
			Duration:        duration,
			Success:         false,
			Checksum:        actualChecksum,
			Error:           fmt.Sprintf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum),
		}
		return res, fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	// Verify destination artifact exists and has matching size
	destInfo, err := os.Stat(destPath)
	if err != nil || destInfo.Size() != srcInfo.Size() {
		res := &TransferResult{
			SourcePath:      sourcePath,
			DestinationPath: destPath,
			TargetNode:      targetNode,
			SizeBytes:       written,
			StartTime:       startTime,
			EndTime:         endTime,
			Duration:        duration,
			Success:         false,
			Checksum:        actualChecksum,
			Error:           "destination file size verification failed",
		}
		return res, fmt.Errorf("destination file verification failed: sizes differ")
	}

	return &TransferResult{
		SourcePath:      sourcePath,
		DestinationPath: destPath,
		TargetNode:      targetNode,
		SizeBytes:       written,
		StartTime:       startTime,
		EndTime:         endTime,
		Duration:        duration,
		Success:         true,
		Checksum:        actualChecksum,
	}, nil
}

// MockTransferProvider provides a simulated transfer provider for unit tests and benchmarks.
type MockTransferProvider struct {
	SimulatedDuration time.Duration
	SimulateFailure   bool
	FailureMessage    string
}

// Transfer simulates artifact transfer.
func (m *MockTransferProvider) Transfer(ctx context.Context, sourcePath, targetNode, destDir, expectedChecksum string) (*TransferResult, error) {
	start := time.Now()
	if m.SimulateFailure {
		msg := m.FailureMessage
		if msg == "" {
			msg = "simulated transfer failure"
		}
		return &TransferResult{
			SourcePath: sourcePath,
			TargetNode: targetNode,
			StartTime:  start,
			EndTime:    time.Now(),
			Success:    false,
			Error:      msg,
		}, fmt.Errorf("%s", msg)
	}

	if m.SimulatedDuration > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.SimulatedDuration):
		}
	}

	destPath := filepath.Join(destDir, filepath.Base(sourcePath))
	now := time.Now()
	return &TransferResult{
		SourcePath:      sourcePath,
		DestinationPath: destPath,
		TargetNode:      targetNode,
		SizeBytes:       42 * 1024 * 1024,
		StartTime:       start,
		EndTime:         now,
		Duration:        now.Sub(start),
		Success:         true,
		Checksum:        expectedChecksum,
	}, nil
}

var _ TransferProvider = (*FileTransferProvider)(nil)
var _ TransferProvider = (*MockTransferProvider)(nil)
