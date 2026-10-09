package migration

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// MigrationState represents the lifecycle state in the MS2M state machine.
type MigrationState string

const (
	StateIdle              MigrationState = "IDLE"
	StatePreparing         MigrationState = "PREPARING"
	StateCheckpointing     MigrationState = "CHECKPOINTING"
	StateCheckpointCreated MigrationState = "CHECKPOINT_CREATED"
	StateTransferring      MigrationState = "TRANSFERRING"
	StateRestoring         MigrationState = "RESTORING"
	StateReplaying         MigrationState = "REPLAYING"
	StateCutoff            MigrationState = "CUTOFF"
	StateFinalizing        MigrationState = "FINALIZING"
	StateCompleted         MigrationState = "COMPLETED"
	StateRollingBack       MigrationState = "ROLLING_BACK"
	StateSourceRestored    MigrationState = "SOURCE_RESTORED"
	StateFailed            MigrationState = "FAILED"
)

// Migration represents a single migration execution lifecycle and associated telemetry.
type Migration struct {
	ID                 string         `json:"migration_id"`
	SourcePod          string         `json:"source_pod"`
	Namespace          string         `json:"namespace"`
	TargetNode         string         `json:"target_node"`
	TargetPod          string         `json:"target_pod,omitempty"`
	Mode               string         `json:"mode"`
	State              MigrationState `json:"state"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	StartTime          time.Time      `json:"start_time,omitempty"`
	EndTime            time.Time      `json:"end_time,omitempty"`
	Downtime           time.Duration  `json:"downtime,omitempty"`
	CheckpointPath     string         `json:"checkpoint_path,omitempty"`
	CheckpointSize     int64          `json:"checkpoint_size_bytes,omitempty"`
	CheckpointDuration time.Duration  `json:"checkpoint_duration,omitempty"`
	TransferDuration   time.Duration  `json:"transfer_duration,omitempty"`
	RestoreDuration    time.Duration  `json:"restore_duration,omitempty"`
	ReplayDuration     time.Duration  `json:"replay_duration,omitempty"`
	Lambda             float64        `json:"arrival_rate_lambda"`
	MuTarget           float64        `json:"target_processing_rate_mu"`
	Utilization        float64        `json:"utilization"`
	CutoffTime         time.Duration  `json:"calculated_cutoff"`
	CutoffTimestamp    time.Time      `json:"cutoff_timestamp,omitempty"`
	LastSequence       int64          `json:"last_sequence_id,omitempty"`
	ReplayedMessages   int64          `json:"messages_replayed"`
	DuplicatesDetected int64          `json:"duplicates_detected"`
	ErrorReason        string         `json:"error_reason,omitempty"`
	RollbackReason     string         `json:"rollback_reason,omitempty"`

	mu sync.RWMutex
}

// SetState safely transitions the migration state and emits formatted logs.
func (m *Migration) SetState(newState MigrationState, detail ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !isValidTransition(m.State, newState) {
		return fmt.Errorf("[%s] illegal state transition from %s to %s", m.ID, m.State, newState)
	}

	m.State = newState
	m.UpdatedAt = time.Now().UTC()

	extra := ""
	if len(detail) > 0 && detail[0] != "" {
		extra = " " + detail[0]
	}
	log.Printf("[%s] %s%s", m.ID, newState, extra)
	return nil
}

// GetState returns the current migration state thread-safely.
func (m *Migration) GetState() MigrationState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.State
}

// MigrationSnapshot represents an immutable thread-safe snapshot of migration state for serialization.
type MigrationSnapshot struct {
	ID                 string         `json:"migration_id"`
	SourcePod          string         `json:"source_pod"`
	Namespace          string         `json:"namespace"`
	TargetNode         string         `json:"target_node"`
	TargetPod          string         `json:"target_pod,omitempty"`
	Mode               string         `json:"mode"`
	State              MigrationState `json:"state"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	StartTime          time.Time      `json:"start_time,omitempty"`
	EndTime            time.Time      `json:"end_time,omitempty"`
	Downtime           time.Duration  `json:"downtime,omitempty"`
	CheckpointPath     string         `json:"checkpoint_path,omitempty"`
	CheckpointSize     int64          `json:"checkpoint_size_bytes,omitempty"`
	CheckpointDuration time.Duration  `json:"checkpoint_duration,omitempty"`
	TransferDuration   time.Duration  `json:"transfer_duration,omitempty"`
	RestoreDuration    time.Duration  `json:"restore_duration,omitempty"`
	ReplayDuration     time.Duration  `json:"replay_duration,omitempty"`
	Lambda             float64        `json:"arrival_rate_lambda"`
	MuTarget           float64        `json:"target_processing_rate_mu"`
	Utilization        float64        `json:"utilization"`
	CutoffTime         time.Duration  `json:"calculated_cutoff"`
	CutoffTimestamp    time.Time      `json:"cutoff_timestamp,omitempty"`
	LastSequence       int64          `json:"last_sequence_id,omitempty"`
	ReplayedMessages   int64          `json:"messages_replayed"`
	DuplicatesDetected int64          `json:"duplicates_detected"`
	ErrorReason        string         `json:"error_reason,omitempty"`
	RollbackReason     string         `json:"rollback_reason,omitempty"`
}

// Snapshot returns a copy of the migration metadata for safe serialization without copying the mutex.
func (m *Migration) Snapshot() MigrationSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return MigrationSnapshot{
		ID:                 m.ID,
		SourcePod:          m.SourcePod,
		Namespace:          m.Namespace,
		TargetNode:         m.TargetNode,
		TargetPod:          m.TargetPod,
		Mode:               m.Mode,
		State:              m.State,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
		StartTime:          m.StartTime,
		EndTime:            m.EndTime,
		Downtime:           m.Downtime,
		CheckpointPath:     m.CheckpointPath,
		CheckpointSize:     m.CheckpointSize,
		CheckpointDuration: m.CheckpointDuration,
		TransferDuration:   m.TransferDuration,
		RestoreDuration:    m.RestoreDuration,
		ReplayDuration:     m.ReplayDuration,
		Lambda:             m.Lambda,
		MuTarget:           m.MuTarget,
		Utilization:        m.Utilization,
		CutoffTime:         m.CutoffTime,
		CutoffTimestamp:    m.CutoffTimestamp,
		LastSequence:       m.LastSequence,
		ReplayedMessages:   m.ReplayedMessages,
		DuplicatesDetected: m.DuplicatesDetected,
		ErrorReason:        m.ErrorReason,
		RollbackReason:     m.RollbackReason,
	}
}

// isValidTransition validates allowable lifecycle progressions.
func isValidTransition(from, to MigrationState) bool {
	if from == to {
		return true
	}
	// Any state can transition to ROLLING_BACK or FAILED upon operational failure
	if to == StateRollingBack || to == StateFailed {
		return true
	}

	switch from {
	case StateIdle:
		return to == StatePreparing
	case StatePreparing:
		return to == StateCheckpointing
	case StateCheckpointing:
		return to == StateCheckpointCreated
	case StateCheckpointCreated:
		return to == StateTransferring
	case StateTransferring:
		return to == StateRestoring
	case StateRestoring:
		return to == StateReplaying
	case StateReplaying:
		return to == StateCutoff
	case StateCutoff:
		return to == StateFinalizing
	case StateFinalizing:
		return to == StateCompleted
	case StateRollingBack:
		return to == StateSourceRestored || to == StateFailed
	case StateSourceRestored:
		return to == StateFailed
	case StateCompleted, StateFailed:
		return false // terminal states
	default:
		return false
	}
}
