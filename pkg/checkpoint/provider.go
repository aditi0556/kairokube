// Package checkpoint provides abstractions and implementations for Forensic Container Checkpointing (FCC)
// in Kubernetes, based on CRIU and the Kubelet checkpoint API.
package checkpoint

import (
	"context"
	"time"
)

// CheckpointStatus represents the state of a checkpoint operation.
type CheckpointStatus string

const (
	CheckpointRequested CheckpointStatus = "CHECKPOINT_REQUESTED"
	CheckpointCreated   CheckpointStatus = "CHECKPOINT_CREATED"
	CheckpointFailed    CheckpointStatus = "CHECKPOINT_FAILED"
)

// CheckpointResult holds metadata and artifact details for a created checkpoint.
type CheckpointResult struct {
	Namespace     string        `json:"namespace"`
	PodName       string        `json:"pod_name"`
	ContainerName string        `json:"container_name"`
	NodeName      string        `json:"node_name"`
	FilePath      string        `json:"file_path"`
	SizeBytes     int64         `json:"size_bytes"`
	CreatedAt     time.Time     `json:"created_at"`
	Duration      time.Duration `json:"duration"`
	Checksum      string        `json:"checksum"` // SHA-256
}

// Target defines the target environment for checkpoint restoration.
type Target struct {
	Namespace string `json:"namespace"`
	PodName   string `json:"pod_name"`
	NodeName  string `json:"node_name"`
}

// CheckpointProvider defines the interface for container checkpointing and restoration.
// Different cluster environments or testing harnesses can provide dedicated implementations.
type CheckpointProvider interface {
	// CreateCheckpoint initiates a checkpoint for the specified container within a pod.
	CreateCheckpoint(ctx context.Context, namespace, podName, containerName string) (*CheckpointResult, error)

	// RestoreCheckpoint restores a checkpointed artifact on the target environment.
	RestoreCheckpoint(ctx context.Context, checkpoint *CheckpointResult, target Target) error
}
