package checkpoint

import (
	"context"
	"fmt"
	"strings"
)

// CRIOImageRestorer represents CRI-O restore through target Pod creation.
// CRI-O detects the checkpoint annotation embedded in the OCI image and
// restores the process during CreateContainer; no Docker command is run.
type CRIOImageRestorer struct{}

func NewCRIOImageRestorer() *CRIOImageRestorer { return &CRIOImageRestorer{} }

func (r *CRIOImageRestorer) RestoreArchive(_ context.Context, archivePath, containerID string) (string, error) {
	if strings.TrimSpace(archivePath) == "" || strings.TrimSpace(containerID) == "" {
		return "", fmt.Errorf("CRI-O checkpoint image and target container are required")
	}
	return containerID, nil
}
