package rabbitmq

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
)

// MigrationRequest is a control-plane command, separate from application data messages.
type MigrationRequest struct {
	ID         string `json:"id"`
	SourcePod  string `json:"source_pod"`
	Namespace  string `json:"namespace"`
	TargetNode string `json:"target_node"`
}

// NewMigrationRequest builds a migration command with a fresh UUID. The ID lets the
// manager ignore duplicate deliveries of the same request.
func NewMigrationRequest(sourcePod, namespace, targetNode string) *MigrationRequest {
	return &MigrationRequest{ID: uuid.New().String(), SourcePod: sourcePod, Namespace: namespace, TargetNode: targetNode}
}
// Encode serializes the request as JSON for publication to the control queue.
func (r *MigrationRequest) Encode() ([]byte, error) { return json.Marshal(r) }

// DecodeMigrationRequest parses and validates a migration request. It requires id,
// source_pod, and target_node, and defaults namespace to "default".
func DecodeMigrationRequest(data []byte) (*MigrationRequest, error) {
	var r MigrationRequest
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("invalid migration request: %w", err)
	}
	if r.ID == "" || r.SourcePod == "" || r.TargetNode == "" {
		return nil, fmt.Errorf("migration request requires id, source_pod, and target_node")
	}
	if r.Namespace == "" {
		r.Namespace = "default"
	}
	return &r, nil
}
