package migration

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/aditi0556/kairokube/pkg/checkpoint"
	"github.com/aditi0556/kairokube/pkg/config"
	"github.com/aditi0556/kairokube/pkg/k8s"
	"github.com/aditi0556/kairokube/pkg/ms2m"
	"github.com/aditi0556/kairokube/pkg/rabbitmq"
	"github.com/aditi0556/kairokube/pkg/transfer"
)

// Manager coordinates the end-to-end MS2M stateful microservice migration lifecycle.
type Manager struct {
	cfg             *config.Config
	k8sClient       *k8s.Client
	rmqClient       *rabbitmq.Client
	checkpoint      checkpoint.CheckpointProvider
	transfer        transfer.TransferProvider
	workloadCtrl    WorkloadController
	rateMonitor     *RealTimeRateMonitor
	rollbackMgr     *RollbackManager
	metrics         *MetricsCollector
	migrations      map[string]*Migration
	mu              sync.RWMutex
	migCounter      int64
	requestMu       sync.Mutex
	handledRequests map[string]struct{}
}

// NewManager constructs a fully configured Migration Manager.
func NewManager(
	cfg *config.Config,
	k8sClient *k8s.Client,
	rmqClient *rabbitmq.Client,
	cp checkpoint.CheckpointProvider,
	tp transfer.TransferProvider,
	wc WorkloadController,
	metrics *MetricsCollector,
) *Manager {
	if cfg == nil {
		cfg = config.Load()
	}
	if metrics == nil {
		metrics = NewMetricsCollector()
	}
	defaultMu := 0.0
	if cfg.MigrationMode != "pod" && os.Getenv("CONSUMER_STATUS_URL") == "" {
		defaultMu = 20.0 // explicit test-only mock capacity
	}
	rm := NewRateMonitor(rmqClient, cfg.MetricsWindow, defaultMu)
	rb := NewRollbackManager(wc, metrics)

	return &Manager{
		cfg:             cfg,
		k8sClient:       k8sClient,
		rmqClient:       rmqClient,
		checkpoint:      cp,
		transfer:        tp,
		workloadCtrl:    wc,
		rateMonitor:     rm,
		rollbackMgr:     rb,
		metrics:         metrics,
		migrations:      make(map[string]*Migration),
		handledRequests: make(map[string]struct{}),
	}
}

// StartMigrationRequestConsumer consumes control-plane migration requests from RabbitMQ.
func (mgr *Manager) StartMigrationRequestConsumer(ctx context.Context, queueName string) error {
	if mgr.rmqClient == nil {
		return fmt.Errorf("RabbitMQ client is not initialized")
	}
	deliveries, err := mgr.rmqClient.Consume(queueName)
	if err != nil {
		return err
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case delivery, ok := <-deliveries:
				if !ok {
					return
				}
				req, decodeErr := rabbitmq.DecodeMigrationRequest(delivery.Body)
				if decodeErr != nil {
					log.Printf("Rejecting migration request: %v", decodeErr)
					_ = delivery.Nack(false, false)
					continue
				}
				mgr.requestMu.Lock()
				_, duplicate := mgr.handledRequests[req.ID]
				if !duplicate {
					mgr.handledRequests[req.ID] = struct{}{}
				}
				mgr.requestMu.Unlock()
				if duplicate {
					log.Printf("Ignoring duplicate migration request %s", req.ID)
					_ = delivery.Ack(false)
					continue
				}
				if _, startErr := mgr.StartMigrationAsync(req.SourcePod, req.Namespace, req.TargetNode); startErr != nil {
					log.Printf("Migration request %s rejected: %v", req.ID, startErr)
					_ = delivery.Nack(false, false)
					continue
				}
				log.Printf("Started migration for request %s: %s/%s -> %s", req.ID, req.Namespace, req.SourcePod, req.TargetNode)
				_ = delivery.Ack(false)
			}
		}
	}()
	return nil
}

// CreateMigration creates a new migration tracking record in IDLE state and
// rejects a second active migration for the same namespace and source Pod.
func (mgr *Manager) CreateMigration(sourcePod, namespace, targetNode string) (*Migration, error) {
	if sourcePod == "" {
		return nil, fmt.Errorf("source_pod cannot be empty")
	}
	if namespace == "" {
		namespace = "default"
	}
	if targetNode == "" {
		return nil, fmt.Errorf("target_node cannot be empty")
	}

	mgr.mu.Lock()
	for _, active := range mgr.migrations {
		snapshot := active.Snapshot()
		if snapshot.Namespace == namespace && snapshot.SourcePod == sourcePod &&
			snapshot.State != StateCompleted && snapshot.State != StateFailed {
			mgr.mu.Unlock()
			return nil, fmt.Errorf("migration %s is already active for source Pod %s/%s", snapshot.ID, namespace, sourcePod)
		}
	}
	mgr.migCounter++
	id := fmt.Sprintf("MIG-%04d", mgr.migCounter)
	mig := &Migration{
		ID:         id,
		SourcePod:  sourcePod,
		Namespace:  namespace,
		TargetNode: targetNode,
		Mode:       mgr.cfg.MigrationMode,
		State:      StateIdle,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	mgr.migrations[id] = mig
	mgr.mu.Unlock()

	return mig, nil
}

// StartMigrationAsync initiates the migration workflow asynchronously in the background.
func (mgr *Manager) StartMigrationAsync(sourcePod, namespace, targetNode string) (*Migration, error) {
	mig, err := mgr.CreateMigration(sourcePod, namespace, targetNode)
	if err != nil {
		return nil, err
	}

	// Transition to PREPARING immediately before launching async runner
	if err := mig.SetState(StatePreparing, fmt.Sprintf("Source: %s/%s, Target: %s", mig.Namespace, mig.SourcePod, mig.TargetNode)); err != nil {
		return nil, err
	}

	mgr.metrics.IncMigrationTotal()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), mgr.cfg.MigrationTimeout)
		defer cancel()

		if err := mgr.executeWorkflow(ctx, mig); err != nil {
			log.Printf("[%s] Workflow finished with error: %v", mig.ID, err)
		}
	}()

	return mig, nil
}

// executeWorkflow executes the 15-step MS2M migration lifecycle.
func (mgr *Manager) executeWorkflow(ctx context.Context, mig *Migration) error {
	mig.StartTime = time.Now().UTC()
	startDowntime := time.Time{}
	if mgr.checkpoint == nil || mgr.transfer == nil || mgr.workloadCtrl == nil {
		err := fmt.Errorf("migration requires checkpoint, transfer, and workload providers")
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StatePreparing, err)
		return err
	}

	// -------------------------------------------------------------------------
	// Step 1 & 2: Validate source Pod and target Node environment
	// -------------------------------------------------------------------------
	if mgr.workloadCtrl != nil {
		if err := mgr.workloadCtrl.ValidateSource(ctx, mig.Namespace, mig.SourcePod); err != nil {
			mgr.rollbackMgr.ExecuteRollback(ctx, mig, StatePreparing, fmt.Errorf("source validation failed: %w", err))
			return err
		}
		if err := mgr.workloadCtrl.ValidateTargetNode(ctx, mig.TargetNode); err != nil {
			mgr.rollbackMgr.ExecuteRollback(ctx, mig, StatePreparing, fmt.Errorf("target node validation failed: %w", err))
			return err
		}
	}

	// -------------------------------------------------------------------------
	// Pre-flight: Feasibility Check & Rate Measurement
	// -------------------------------------------------------------------------
	lambda, mu, queueDepth, _ := mgr.rateMonitor.MeasureRates(ctx, mgr.cfg.QueueName)
	mgr.metrics.SetQueueDepth(queueDepth)
	mgr.metrics.SetRates(lambda, mu)

	feasibility := ms2m.EvaluateFeasibility(lambda, mu, mgr.cfg.FeasibilityPolicy)
	log.Printf("[%s] Migration feasibility: arrival_rate=%.2f msg/s, target_rate=%.2f msg/s, utilization=%.1f%%, status=%s, policy=%s",
		mig.ID, feasibility.ArrivalRate, feasibility.TargetRate, feasibility.Utilization*100, feasibility.Status, feasibility.Policy)

	if !feasibility.CanProceed {
		err := fmt.Errorf("migration rejected by policy %q: %s", mgr.cfg.FeasibilityPolicy, feasibility.Message)
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StatePreparing, err)
		return err
	}

	// -------------------------------------------------------------------------
	// Step 4 & 5: Checkpointing (FCC)
	// -------------------------------------------------------------------------
	if err := mig.SetState(StateCheckpointing, "starting Forensic Container Checkpoint"); err != nil {
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StatePreparing, err)
		return err
	}

	ckptCtx, ckptCancel := context.WithTimeout(ctx, mgr.cfg.CheckpointTimeout)
	ckptResult, err := mgr.checkpoint.CreateCheckpoint(ckptCtx, mig.Namespace, mig.SourcePod, "")
	ckptCancel()

	if err != nil {
		log.Printf("[%s] CHECKPOINT_FAILED: %v", mig.ID, err)
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateCheckpointing, fmt.Errorf("checkpoint creation failed: %w", err))
		return err
	}

	mig.mu.Lock()
	mig.CheckpointPath = ckptResult.FilePath
	mig.CheckpointSize = ckptResult.SizeBytes
	mig.CheckpointDuration = ckptResult.Duration
	mig.mu.Unlock()

	log.Printf("[%s] CHECKPOINT_CREATED artifact=%s size=%d bytes duration=%s",
		mig.ID, ckptResult.FilePath, ckptResult.SizeBytes, ckptResult.Duration)
	_ = mig.SetState(StateCheckpointCreated)

	// -------------------------------------------------------------------------
	// Step 6: Transfer Checkpoint Artifact
	// -------------------------------------------------------------------------
	if err := mig.SetState(StateTransferring, fmt.Sprintf("transferring %s to node %s", ckptResult.FilePath, mig.TargetNode)); err != nil {
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateCheckpointCreated, err)
		return err
	}

	txCtx, txCancel := context.WithTimeout(ctx, mgr.cfg.TransferTimeout)
	txResult, err := mgr.transfer.Transfer(txCtx, ckptResult.FilePath, mig.TargetNode, mgr.cfg.CheckpointDir, ckptResult.Checksum)
	txCancel()

	if err != nil {
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateTransferring, fmt.Errorf("transfer failed: %w", err))
		return err
	}

	mig.mu.Lock()
	mig.TransferDuration = txResult.Duration
	mig.mu.Unlock()

	log.Printf("[%s] Transfer completed: destination=%s duration=%s", mig.ID, txResult.DestinationPath, txResult.Duration)

	// -------------------------------------------------------------------------
	// Step 7: Restore Target Workload
	// -------------------------------------------------------------------------
	if err := mig.SetState(StateRestoring, fmt.Sprintf("target=%s", mig.TargetNode)); err != nil {
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateTransferring, err)
		return err
	}
	restoreStart := time.Now()

	var targetPodName string
	if mgr.workloadCtrl != nil {
		targetRef := checkpoint.Target{
			Namespace: mig.Namespace,
			PodName:   mig.SourcePod + "-target",
			NodeName:  mig.TargetNode,
		}
		if preflight, ok := mgr.checkpoint.(checkpoint.RestorePreflight); ok {
			if err := preflight.ValidateRestore(ctx, ckptResult, targetRef); err != nil {
				mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateRestoring, fmt.Errorf("restore preflight failed: %w", err))
				return err
			}
		}
		var prepErr error
		targetPodName, prepErr = mgr.workloadCtrl.PrepareTarget(ctx, mig.Namespace, mig.SourcePod, mig.TargetNode)
		if prepErr != nil {
			mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateRestoring, fmt.Errorf("target preparation failed: %w", prepErr))
			return prepErr
		}
		mig.mu.Lock()
		mig.TargetPod = targetPodName
		mig.mu.Unlock()

		targetRef.PodName = targetPodName

		if resErr := mgr.checkpoint.RestoreCheckpoint(ctx, ckptResult, targetRef); resErr != nil {
			mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateRestoring, fmt.Errorf("checkpoint restore failed: %w", resErr))
			return resErr
		}

		// Verify target reaches Ready status
		if verErr := mgr.workloadCtrl.VerifyTarget(ctx, mig.Namespace, targetPodName, mgr.cfg.RestoreTimeout); verErr != nil {
			mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateRestoring, fmt.Errorf("target readiness verification failed: %w", verErr))
			return verErr
		}
	}
	mig.mu.Lock()
	mig.RestoreDuration = time.Since(restoreStart)
	mig.mu.Unlock()

	// -------------------------------------------------------------------------
	// Step 8 & 9: Message Replay & Adaptive Cutoff Calculation
	// -------------------------------------------------------------------------
	if err := mig.SetState(StateReplaying, fmt.Sprintf("monitoring replay, queue_depth=%d", queueDepth)); err != nil {
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateRestoring, err)
		return err
	}
	replayStart := time.Now()

	// Recalculate dynamic arrival and processing rates for adaptive cutoff
	lambda, mu, queueDepth, _ = mgr.rateMonitor.MeasureRates(ctx, mgr.cfg.QueueName)
	cutoffRes := ms2m.CalculateAdaptiveCutoff(lambda, mu, mgr.cfg.MaxReplayTime, mgr.cfg.MinCutoffTime, mgr.cfg.MaxCutoffTime)

	mig.mu.Lock()
	mig.Lambda = cutoffRes.Lambda
	mig.MuTarget = cutoffRes.MuTarget
	mig.Utilization = cutoffRes.Utilization
	mig.CutoffTime = cutoffRes.CalculatedCutoff
	mig.mu.Unlock()

	log.Printf("[%s] λ=%.2f msg/s", mig.ID, cutoffRes.Lambda)
	log.Printf("[%s] μ_target=%.2f msg/s", mig.ID, cutoffRes.MuTarget)
	log.Printf("[%s] utilization=%.3f", mig.ID, cutoffRes.Utilization)
	log.Printf("[%s] max_replay_time=%s", mig.ID, cutoffRes.MaxReplayTime)
	log.Printf("[%s] calculated_cutoff=%s", mig.ID, cutoffRes.CalculatedCutoff)

	// Wait for the dynamically computed adaptive cutoff duration or until context cancellation
	select {
	case <-ctx.Done():
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateReplaying, ctx.Err())
		return ctx.Err()
	case <-time.After(cutoffRes.CalculatedCutoff):
	}

	mig.mu.Lock()
	mig.ReplayDuration = time.Since(replayStart)
	mig.mu.Unlock()

	// -------------------------------------------------------------------------
	// Step 10 & 11: Cutoff & Source Stop
	// -------------------------------------------------------------------------
	startDowntime = time.Now()
	if err := mig.SetState(StateCutoff, fmt.Sprintf("stopping source pod %s", mig.SourcePod)); err != nil {
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateReplaying, err)
		return err
	}

	mig.mu.Lock()
	mig.CutoffTimestamp = time.Now().UTC()
	mig.mu.Unlock()

	if mgr.workloadCtrl != nil {
		if err := mgr.workloadCtrl.StopSource(ctx, mig.Namespace, mig.SourcePod); err != nil {
			mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateCutoff, fmt.Errorf("failed to stop source pod: %w", err))
			return err
		}
	}

	// -------------------------------------------------------------------------
	// Step 12 & 13: Finalizing & Target State Synchronization
	// -------------------------------------------------------------------------
	if err := mig.SetState(StateFinalizing, "verifying target state sync"); err != nil {
		mgr.rollbackMgr.ExecuteRollback(ctx, mig, StateCutoff, err)
		return err
	}

	// Allow remaining in-flight unacknowledged RabbitMQ messages to drain to target
	drainTimeout := 500 * time.Millisecond
	time.Sleep(drainTimeout)

	// -------------------------------------------------------------------------
	// Step 14 & 15: Mark Migration COMPLETED
	// -------------------------------------------------------------------------
	now := time.Now().UTC()
	mig.mu.Lock()
	mig.EndTime = now
	if !startDowntime.IsZero() {
		mig.Downtime = now.Sub(startDowntime)
	}
	mig.mu.Unlock()

	mgr.metrics.IncMigrationSuccess()
	mgr.metrics.RecordMigrationRun(mig)
	if err := mig.SetState(StateCompleted, fmt.Sprintf("downtime=%s duration=%s", mig.Downtime, mig.EndTime.Sub(mig.StartTime))); err != nil {
		return err
	}
	return nil
}

// GetMigration retrieves a migration by its ID.
func (mgr *Manager) GetMigration(id string) (*Migration, bool) {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	mig, ok := mgr.migrations[id]
	return mig, ok
}

// ListMigrations returns all migration records.
func (mgr *Manager) ListMigrations() []*Migration {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	list := make([]*Migration, 0, len(mgr.migrations))
	for _, m := range mgr.migrations {
		list = append(list, m)
	}
	return list
}

// GetRateMonitor returns the RateMonitor instance.
func (mgr *Manager) GetRateMonitor() *RealTimeRateMonitor {
	return mgr.rateMonitor
}

// GetMetrics returns the MetricsCollector instance.
func (mgr *Manager) GetMetrics() *MetricsCollector {
	return mgr.metrics
}
