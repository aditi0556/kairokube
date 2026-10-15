package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CommandRunner executes an external runtime command and returns its output.
// It is injected so restore logic can be tested without Docker or containerd.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultCommandRunner executes commands through the local operating system.
var DefaultCommandRunner CommandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// ErrNodeRestoreUnavailable is returned when no node-side restorer is configured.
var ErrNodeRestoreUnavailable = errors.New("node-side restorer is not configured; live restore is unavailable")

// NodeRestorer restores a checkpoint archive on the node that holds it and returns
// the ID of the restored container. Implementations run on the node (or through
// it), never through the Kubelet API, which only creates archives.
type NodeRestorer interface {
	RestoreArchive(ctx context.Context, archivePath, containerID string) (string, error)
}

// CtrRestorer restores archives with containerd's own tools:
//
//	ctr -n k8s.io containers restore --live <containerID> <ref>
//
// The ref is the checkpoint image produced by `ctr containers checkpoint --task`.
// This path is implemented but not verified on a cluster; see the project guide.
type CtrRestorer struct {
	run CommandRunner
}

// NewCtrRestorer creates a CtrRestorer that executes ctr through run.
// If run is nil, DefaultCommandRunner is used.
func NewCtrRestorer(run CommandRunner) *CtrRestorer {
	if run == nil {
		run = DefaultCommandRunner
	}
	return &CtrRestorer{run: run}
}

// RestoreArchive imports archivePath as a checkpoint image reference and restores
// it with --live into containerID. It returns the container ID reported by ctr,
// or the requested ID when ctr prints nothing.
func (r *CtrRestorer) RestoreArchive(ctx context.Context, archivePath, containerID string) (string, error) {
	if r == nil || r.run == nil {
		return "", ErrNodeRestoreUnavailable
	}
	if strings.TrimSpace(archivePath) == "" || strings.TrimSpace(containerID) == "" {
		return "", errors.New("archive path and container ID are required")
	}
	ref := "localhost/kairokube/restore-" + containerID + ":latest"
	out, err := r.run(ctx, "ctr", "-n", "k8s.io", "containers", "restore", "--live", containerID, ref)
	if err != nil {
		return "", fmt.Errorf("ctr containers restore failed: %w", err)
	}
	if id := strings.TrimSpace(string(out)); id != "" {
		return id, nil
	}
	return containerID, nil
}

// Preflight describes the node-side prerequisites that a live restore needs.
// Each field is checked before any restore is attempted.
type Preflight struct {
	ArchivePath string
	CRIUPath    string
}

// CheckPreflight verifies the archive is present and non-empty and that the CRIU
// binary exists, so a restore fails with a clear reason before it starts.
func CheckPreflight(p Preflight) error {
	if strings.TrimSpace(p.ArchivePath) == "" {
		return errors.New("preflight: checkpoint archive path is empty")
	}
	info, err := os.Stat(p.ArchivePath)
	if err != nil {
		return fmt.Errorf("preflight: checkpoint archive not readable: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("preflight: checkpoint archive %s is empty", p.ArchivePath)
	}
	if strings.TrimSpace(p.CRIUPath) == "" {
		return errors.New("preflight: CRIU path is empty")
	}
	if _, err := os.Stat(p.CRIUPath); err != nil {
		return fmt.Errorf("preflight: CRIU binary not found at %s: %w", p.CRIUPath, err)
	}
	return nil
}
