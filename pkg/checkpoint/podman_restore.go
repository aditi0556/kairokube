package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// CommandRunner executes an external command and returns its standard output.
// It is injected so restore logic can be tested without a container runtime.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// PodmanRestorer restores a CRIU checkpoint archive into a new container using
// `podman container restore --import`. It is a node-side helper: the Kubelet
// checkpoint API creates archives, but it does not restore them.
//
// Podman must run as root for checkpointing and restoring; rootless podman
// cannot checkpoint containers (verified in this environment).
type PodmanRestorer struct {
	run CommandRunner
}

// ErrRestoreUnavailable is returned when no command runner is configured.
var ErrRestoreUnavailable = errors.New("podman restore helper has no command runner configured")

// NewPodmanRestorer creates a restorer that executes commands with run.
func NewPodmanRestorer(run CommandRunner) *PodmanRestorer {
	return &PodmanRestorer{run: run}
}

// Restore imports archivePath as a new container named name and returns the
// container ID reported by podman. It validates inputs before running anything,
// and never reports success unless podman returned a non-empty ID.
func (p *PodmanRestorer) Restore(ctx context.Context, archivePath, name string) (string, error) {
	if p == nil || p.run == nil {
		return "", ErrRestoreUnavailable
	}
	if strings.TrimSpace(archivePath) == "" {
		return "", errors.New("checkpoint archive path is required")
	}
	if strings.TrimSpace(name) == "" {
		return "", errors.New("restored container name is required")
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return "", fmt.Errorf("checkpoint archive not readable: %w", err)
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("checkpoint archive %s is empty", archivePath)
	}

	out, err := p.run(ctx, "podman", "container", "restore", "--import", archivePath, "--name", name)
	if err != nil {
		return "", fmt.Errorf("podman container restore failed: %w", err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", errors.New("podman container restore returned no container ID")
	}
	return id, nil
}
