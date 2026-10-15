//go:build ignore

// Temporary local probe used to verify the dashboard page and its vendored
// Three.js assets are served correctly. Not part of the build.
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aditi0556/kairokube/pkg/checkpoint"
	"github.com/aditi0556/kairokube/pkg/config"
	"github.com/aditi0556/kairokube/pkg/migration"
	"github.com/aditi0556/kairokube/pkg/transfer"
)

func main() {
	dir, _ := os.MkdirTemp("", "kairokube-probe")
	mgr := migration.NewManager(&config.Config{
		MigrationMode:     "mock",
		FeasibilityPolicy: "warn",
		MaxReplayTime:     time.Second,
		MinCutoffTime:     100 * time.Millisecond,
		MaxCutoffTime:     time.Second,
		MetricsWindow:     time.Second,
		CheckpointTimeout: 5 * time.Second,
		TransferTimeout:   5 * time.Second,
		RestoreTimeout:    5 * time.Second,
		MigrationTimeout:  10 * time.Second,
	}, nil, nil,
		checkpoint.NewMockCheckpointProvider(dir), &transfer.MockTransferProvider{},
		migration.NewMockWorkloadController(), migration.NewMetricsCollector())
	srv := migration.NewServer(mgr, "127.0.0.1:18099")
	fmt.Println("probe listening on 127.0.0.1:18099")
	log.Fatal(srv.Start())
}
