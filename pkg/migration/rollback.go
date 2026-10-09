package migration

import (
	"context"
	"fmt"
	"log"
	"time"
)

// RollbackManager orchestrates safe compensation steps when a migration fails.
type RollbackManager struct {
	workloadCtrl WorkloadController
	metrics      *MetricsCollector
}

// NewRollbackManager creates a RollbackManager.
func NewRollbackManager(ctrl WorkloadController, metrics *MetricsCollector) *RollbackManager {
	return &RollbackManager{
		workloadCtrl: ctrl,
		metrics:      metrics,
	}
}

// ExecuteRollback handles compensation actions for a failed migration.
// Lifecycle: [STATE] -> ROLLING_BACK -> SOURCE_RESTORED -> FAILED.
func (r *RollbackManager) ExecuteRollback(ctx context.Context, mig *Migration, failedAt MigrationState, reason error) {
	if mig == nil {
		return
	}

	mig.mu.Lock()
	mig.ErrorReason = reason.Error()
	mig.RollbackReason = fmt.Sprintf("failed during %s: %v", failedAt, reason)
	mig.EndTime = time.Now().UTC()
	mig.mu.Unlock()

	_ = mig.SetState(StateRollingBack, fmt.Sprintf("initiating rollback due to: %v", reason))
	var compensationErr error

	// 1. Cleanup target workload if one was scheduled
	if mig.TargetPod != "" && r.workloadCtrl != nil {
		log.Printf("[%s] Rollback: Cleaning up unready/failed target pod %s/%s", mig.ID, mig.Namespace, mig.TargetPod)
		if err := r.workloadCtrl.CleanupTarget(ctx, mig.Namespace, mig.TargetPod); err != nil {
			log.Printf("[%s] Rollback warning: failed to delete target pod: %v", mig.ID, err)
			compensationErr = fmt.Errorf("target cleanup failed: %w", err)
		}
	}

	// 2. Restore source processing if migration progressed to or past CUTOFF
	if failedAt == StateCutoff || failedAt == StateFinalizing {
		if r.workloadCtrl != nil {
			log.Printf("[%s] Rollback: Restoring source pod %s/%s", mig.ID, mig.Namespace, mig.SourcePod)
			if err := r.workloadCtrl.RestoreSource(ctx, mig.Namespace, mig.SourcePod); err != nil {
				log.Printf("[%s] Rollback warning: failed to restore source pod: %v", mig.ID, err)
				if compensationErr != nil {
					compensationErr = fmt.Errorf("%v; source restoration failed: %w", compensationErr, err)
				} else {
					compensationErr = fmt.Errorf("source restoration failed: %w", err)
				}
			}
		}
	}

	if r.metrics != nil {
		r.metrics.IncMigrationFailure()
		r.metrics.RecordMigrationRun(mig)
	}

	// Do not label an incomplete compensation as SOURCE_RESTORED.
	if compensationErr != nil {
		mig.mu.Lock()
		mig.ErrorReason = fmt.Sprintf("%s; rollback incomplete: %v", mig.ErrorReason, compensationErr)
		mig.RollbackReason = fmt.Sprintf("%s; rollback incomplete: %v", mig.RollbackReason, compensationErr)
		mig.mu.Unlock()
		_ = mig.SetState(StateFailed, fmt.Sprintf("migration aborted and rollback incomplete: %v", compensationErr))
	} else {
		_ = mig.SetState(StateSourceRestored, "source remains active or restoration completed; unacknowledged messages rely on broker redelivery")
		_ = mig.SetState(StateFailed, fmt.Sprintf("migration aborted: %v", reason))
	}

}
