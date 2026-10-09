package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aditi0556/kairokube/pkg/checkpoint"
	"github.com/aditi0556/kairokube/pkg/config"
	"github.com/aditi0556/kairokube/pkg/migration"
	"github.com/aditi0556/kairokube/pkg/transfer"
)

// ExperimentResult stores telemetry metrics for a single migration experiment run.
type ExperimentResult struct {
	TargetRateLambda      float64 `json:"target_lambda_msg_s"`
	MeasuredLambda        float64 `json:"measured_lambda_msg_s"`
	MeasuredMu            float64 `json:"measured_mu_msg_s"`
	Utilization           float64 `json:"utilization"`
	CalculatedCutoffSec   float64 `json:"calculated_cutoff_seconds"`
	TotalMigrationTimeSec float64 `json:"total_migration_time_seconds"`
	DowntimeSec           float64 `json:"downtime_seconds"`
	CheckpointDurationSec float64 `json:"checkpoint_duration_seconds"`
	TransferDurationSec   float64 `json:"transfer_duration_seconds"`
	RestoreDurationSec    float64 `json:"restore_duration_seconds"`
	ReplayDurationSec     float64 `json:"replay_duration_seconds"`
	QueueDepth            int64   `json:"queue_depth"`
	MessagesReplayed      int64   `json:"messages_replayed"`
	DuplicatesDetected    int64   `json:"duplicates_detected"`
	Status                string  `json:"status"`
}

func main() {
	log.Println("==================================================================")
	log.Println("MS2M Kubernetes Microservice Migration Benchmark Runner")
	log.Println("Evaluating rates: 4, 8, 12, 16, 20 msg/sec")
	log.Println("==================================================================")

	rates := []float64{4.0, 8.0, 12.0, 16.0, 20.0}
	results := make([]ExperimentResult, 0, len(rates))

	tempDir := filepath.Join(os.TempDir(), "kairokube-benchmark")
	_ = os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	for _, rate := range rates {
		log.Printf("\n>>> Running Experiment at lambda = %.1f msg/s (target processing mu = 20.0 msg/s) <<<", rate)

		res := runExperiment(rate, tempDir)
		results = append(results, res)

		log.Printf("Result for lambda=%.1f: TotalTime=%.3fs Downtime=%.3fs Cutoff=%.3fs Status=%s",
			rate, res.TotalMigrationTimeSec, res.DowntimeSec, res.CalculatedCutoffSec, res.Status)
	}

	// 1. Save JSON
	jsonPath := "benchmark_results.json"
	jsonData, err := json.MarshalIndent(results, "", "  ")
	if err == nil {
		_ = os.WriteFile(jsonPath, jsonData, 0644)
		log.Printf("\nSaved JSON benchmark results to %s", jsonPath)
	}

	// 2. Save CSV
	csvPath := "benchmark_results.csv"
	csvFile, err := os.Create(csvPath)
	if err == nil {
		defer csvFile.Close()
		w := csv.NewWriter(csvFile)
		_ = w.Write([]string{
			"TargetRateLambda", "MeasuredLambda", "MeasuredMu", "Utilization",
			"CalculatedCutoffSec", "TotalMigrationTimeSec", "DowntimeSec",
			"CheckpointDurationSec", "TransferDurationSec", "RestoreDurationSec",
			"ReplayDurationSec", "QueueDepth", "MessagesReplayed", "DuplicatesDetected", "Status",
		})
		for _, r := range results {
			_ = w.Write([]string{
				fmt.Sprintf("%.1f", r.TargetRateLambda),
				fmt.Sprintf("%.2f", r.MeasuredLambda),
				fmt.Sprintf("%.2f", r.MeasuredMu),
				fmt.Sprintf("%.3f", r.Utilization),
				fmt.Sprintf("%.3f", r.CalculatedCutoffSec),
				fmt.Sprintf("%.3f", r.TotalMigrationTimeSec),
				fmt.Sprintf("%.3f", r.DowntimeSec),
				fmt.Sprintf("%.3f", r.CheckpointDurationSec),
				fmt.Sprintf("%.3f", r.TransferDurationSec),
				fmt.Sprintf("%.3f", r.RestoreDurationSec),
				fmt.Sprintf("%.3f", r.ReplayDurationSec),
				fmt.Sprintf("%d", r.QueueDepth),
				fmt.Sprintf("%d", r.MessagesReplayed),
				fmt.Sprintf("%d", r.DuplicatesDetected),
				r.Status,
			})
		}
		w.Flush()
		log.Printf("Saved CSV benchmark results to %s", csvPath)
	}

	// Print summary table
	fmt.Println("\n========================================================================================================================")
	fmt.Println("λ (msg/s) | μ (msg/s) | Util   | Cutoff (s) | Total Time (s) | Downtime (s) | Checkpoint (s) | Transfer (s) | Replay (s) | Status")
	fmt.Println("------------------------------------------------------------------------------------------------------------------------")
	for _, r := range results {
		fmt.Printf("%-9.1f | %-9.1f | %-6.2f | %-10.2f | %-14.3f | %-12.3f | %-14.3f | %-12.3f | %-10.3f | %s\n",
			r.TargetRateLambda, r.MeasuredMu, r.Utilization, r.CalculatedCutoffSec,
			r.TotalMigrationTimeSec, r.DowntimeSec, r.CheckpointDurationSec,
			r.TransferDurationSec, r.ReplayDurationSec, r.Status)
	}
	fmt.Println("========================================================================================================================")
}

func runExperiment(targetLambda float64, tempDir string) ExperimentResult {
	cfg := &config.Config{
		MaxReplayTime:     5 * time.Second,
		MinCutoffTime:     500 * time.Millisecond,
		MaxCutoffTime:     15 * time.Second,
		MetricsWindow:     2 * time.Second,
		CheckpointTimeout: 10 * time.Second,
		TransferTimeout:   10 * time.Second,
		RestoreTimeout:    10 * time.Second,
		MigrationTimeout:  30 * time.Second,
		FeasibilityPolicy: "warn",
		CheckpointDir:     tempDir,
	}

	cp := checkpoint.NewMockCheckpointProvider(tempDir)
	tp := &transfer.MockTransferProvider{
		SimulatedDuration: 100 * time.Millisecond,
	}
	wc := migration.NewMockWorkloadController()
	metrics := migration.NewMetricsCollector()

	mgr := migration.NewManager(cfg, nil, nil, cp, tp, wc, metrics)

	// Feed simulated traffic into rate monitor to establish lambda
	windowSeconds := cfg.MetricsWindow.Seconds()
	messagesInWindow := int64(targetLambda * windowSeconds)
	mgr.GetRateMonitor().RecordArrival(messagesInWindow)
	mgr.GetRateMonitor().RecordProcessing(int64(20.0 * windowSeconds)) // target capacity 20 msg/s

	mig, err := mgr.StartMigrationAsync("consumer-0", "default", "worker-2")
	if err != nil {
		return ExperimentResult{
			TargetRateLambda: targetLambda,
			Status:           fmt.Sprintf("FAILED: %v", err),
		}
	}

	// Poll until terminal state
	deadline := time.Now().Add(cfg.MigrationTimeout)
	for time.Now().Before(deadline) {
		st := mig.GetState()
		if st == migration.StateCompleted || st == migration.StateFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	snap := mig.Snapshot()
	status := string(snap.State)
	if snap.State == migration.StateFailed {
		status = fmt.Sprintf("FAILED: %s", snap.ErrorReason)
	}

	return ExperimentResult{
		TargetRateLambda:      targetLambda,
		MeasuredLambda:        snap.Lambda,
		MeasuredMu:            snap.MuTarget,
		Utilization:           snap.Utilization,
		CalculatedCutoffSec:   snap.CutoffTime.Seconds(),
		TotalMigrationTimeSec: snap.EndTime.Sub(snap.StartTime).Seconds(),
		DowntimeSec:           snap.Downtime.Seconds(),
		CheckpointDurationSec: snap.CheckpointDuration.Seconds(),
		TransferDurationSec:   snap.TransferDuration.Seconds(),
		RestoreDurationSec:    snap.RestoreDuration.Seconds(),
		ReplayDurationSec:     snap.ReplayDuration.Seconds(),
		QueueDepth:            0,
		MessagesReplayed:      int64(targetLambda * snap.ReplayDuration.Seconds()),
		DuplicatesDetected:    0,
		Status:                status,
	}
}
