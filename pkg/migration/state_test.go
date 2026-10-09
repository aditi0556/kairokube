package migration

import (
	"testing"
	"time"
)

func TestStateTransitions(t *testing.T) {
	mig := &Migration{
		ID:        "MIG-TEST-01",
		SourcePod: "consumer-0",
		Namespace: "default",
		State:     StateIdle,
		CreatedAt: time.Now(),
	}

	// Normal progression
	progression := []MigrationState{
		StatePreparing,
		StateCheckpointing,
		StateCheckpointCreated,
		StateTransferring,
		StateRestoring,
		StateReplaying,
		StateCutoff,
		StateFinalizing,
		StateCompleted,
	}

	for _, nextState := range progression {
		if err := mig.SetState(nextState); err != nil {
			t.Fatalf("failed valid transition to %s: %v", nextState, err)
		}
		if mig.GetState() != nextState {
			t.Fatalf("expected state %s, got %s", nextState, mig.GetState())
		}
	}

	// Illegal transition from terminal COMPLETED
	if err := mig.SetState(StatePreparing); err == nil {
		t.Errorf("expected error for illegal transition from COMPLETED to PREPARING")
	}
}

func TestRollbackStateTransition(t *testing.T) {
	mig := &Migration{
		ID:        "MIG-TEST-02",
		SourcePod: "consumer-0",
		Namespace: "default",
		State:     StateReplaying,
		CreatedAt: time.Now(),
	}

	// Can transition from REPLAYING to ROLLING_BACK
	if err := mig.SetState(StateRollingBack, "failure occurred"); err != nil {
		t.Fatalf("failed transition to ROLLING_BACK: %v", err)
	}

	// Can transition from ROLLING_BACK to SOURCE_RESTORED
	if err := mig.SetState(StateSourceRestored); err != nil {
		t.Fatalf("failed transition to SOURCE_RESTORED: %v", err)
	}

	// Can transition from SOURCE_RESTORED to FAILED
	if err := mig.SetState(StateFailed); err != nil {
		t.Fatalf("failed transition to FAILED: %v", err)
	}

	// Cannot transition from terminal FAILED
	if err := mig.SetState(StatePreparing); err == nil {
		t.Errorf("expected error transitioning from terminal FAILED")
	}
}
