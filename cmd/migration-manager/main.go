package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aditi0556/kairokube/pkg/checkpoint"
	"github.com/aditi0556/kairokube/pkg/config"
	"github.com/aditi0556/kairokube/pkg/k8s"
	"github.com/aditi0556/kairokube/pkg/migration"
	"github.com/aditi0556/kairokube/pkg/rabbitmq"
	"github.com/aditi0556/kairokube/pkg/transfer"
)

func main() {
	log.Println("Initializing MS2M Migration Manager...")

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	sourcePodFlag := flag.String("source", "", "Source Pod name to migrate (optional CLI mode)")
	targetNodeFlag := flag.String("target", "", "Target node name (optional CLI mode)")
	namespaceFlag := flag.String("namespace", "default", "Kubernetes namespace")
	serverModeFlag := flag.Bool("server", true, "Run HTTP REST API server")
	flag.Parse()

	// 1. Initialize Kubernetes Client
	k8sClient, err := k8s.NewClient()
	if err != nil {
		if cfg.MigrationMode == "pod" || cfg.CheckpointProvider == "kubelet" {
			log.Fatalf("Kubernetes is required by MIGRATION_MODE=%q or CHECKPOINT_PROVIDER=%q: %v", cfg.MigrationMode, cfg.CheckpointProvider, err)
		}
		log.Printf("Using explicitly selected mock workload controller; Kubernetes client unavailable: %v", err)
	} else {
		log.Println("Kubernetes client initialized successfully")
	}

	// 2. Initialize Checkpoint Provider
	var cp checkpoint.CheckpointProvider
	if cfg.CheckpointProvider == "kubelet" {
		if k8sClient == nil {
			log.Fatal("CHECKPOINT_PROVIDER=kubelet requires a working Kubernetes client")
		}
		log.Printf("Configuring Kubelet FCC Checkpoint Provider (port %d)", cfg.KubeletPort)
		cp = checkpoint.NewKubeletFCCProvider(k8sClient.Clientset, k8sClient.RestConfig, checkpoint.KubeletFCCConfig{
			UseNodeProxy: true,
			KubeletPort:  cfg.KubeletPort,
		})
	} else if cfg.CheckpointProvider == "mock" {
		log.Printf("Configuring MOCK checkpoint provider in directory %s; this is simulated state, not a process checkpoint", cfg.CheckpointDir)
		cp = checkpoint.NewMockCheckpointProvider(cfg.CheckpointDir)
	} else {
		log.Fatalf("Unsupported checkpoint provider %q", cfg.CheckpointProvider)
	}

	// 3. Initialize Transfer Provider
	var tp transfer.TransferProvider
	if cfg.TransferProvider == "mock" {
		tp = &transfer.MockTransferProvider{}
	} else {
		tp = transfer.NewFileTransferProvider(cfg.CheckpointDir)
	}

	// 4. Initialize Workload Controller
	var wc migration.WorkloadController
	if cfg.MigrationMode == "mock" {
		wc = migration.NewMockWorkloadController()
	} else if k8sClient != nil && k8sClient.Clientset != nil {
		if cfg.MigrationMode == "statefulset" {
			log.Println("Configuring StatefulSet workload controller")
			wc = migration.NewStatefulSetWorkloadController(k8sClient.Clientset)
		} else {
			log.Println("Configuring Pod workload controller")
			wc = migration.NewPodWorkloadController(k8sClient.Clientset)
		}
	} else {
		log.Fatal("No workload controller available; select MIGRATION_MODE=mock for local simulation")
	}

	// 5. Initialize RabbitMQ Client
	rmqClient, err := rabbitmq.NewClient(cfg.RabbitMQURL)
	if err != nil {
		log.Fatalf("RabbitMQ is required for migration measurements and workload messaging (%s): %v", cfg.RabbitMQURL, err)
	} else {
		log.Println("Connected to RabbitMQ broker successfully")
		defer rmqClient.Close()
	}

	metrics := migration.NewMetricsCollector()
	mgr := migration.NewManager(cfg, k8sClient, rmqClient, cp, tp, wc, metrics)

	// CLI Mode execution if source and target are provided
	if *sourcePodFlag != "" && *targetNodeFlag != "" {
		log.Printf("Executing single migration via CLI: %s -> %s (namespace: %s)", *sourcePodFlag, *targetNodeFlag, *namespaceFlag)
		mig, err := mgr.StartMigrationAsync(*sourcePodFlag, *namespaceFlag, *targetNodeFlag)
		if err != nil {
			log.Fatalf("Failed to start migration: %v", err)
		}

		for {
			st := mig.GetState()
			if st == migration.StateCompleted {
				log.Printf("CLI Migration [%s] completed successfully!", mig.ID)
				os.Exit(0)
			}
			if st == migration.StateFailed {
				snap := mig.Snapshot()
				log.Fatalf("CLI Migration [%s] failed: %s", mig.ID, snap.ErrorReason)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Server Mode
	if *serverModeFlag {
		serverAddr := fmt.Sprintf(":%s", cfg.ServerPort)
		server := migration.NewServer(mgr, serverAddr)

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

		go func() {
			if err := server.Start(); err != nil {
				log.Printf("HTTP server terminated: %v", err)
			}
		}()

		<-stop
		log.Println("Shutdown signal received. Stopping Migration Manager server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
		log.Println("Migration Manager server stopped cleanly.")
	}
}
