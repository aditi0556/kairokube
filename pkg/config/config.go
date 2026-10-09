// Package config provides centralized configuration management for the MS2M
// migration manager and related microservices.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration parameters for the Migration Manager.
type Config struct {
	// RabbitMQ settings
	RabbitMQURL string
	QueueName   string

	// MS2M Replay and Cutoff settings
	MaxReplayTime time.Duration
	MinCutoffTime time.Duration
	MaxCutoffTime time.Duration
	MetricsWindow time.Duration

	// Phase Timeouts
	CheckpointTimeout time.Duration
	TransferTimeout   time.Duration
	RestoreTimeout    time.Duration
	ReplayTimeout     time.Duration
	MigrationTimeout  time.Duration

	// Migration Policies and Modes
	MigrationMode     string // "mock", standalone "pod", or unsupported "statefulset"
	FeasibilityPolicy string // "warn", "reject", "force"

	// Providers
	CheckpointProvider string // "kubelet" or "mock"
	TransferProvider   string // "file" (local/shared path) or "mock"

	// Kubelet FCC Settings
	KubeletPort   int
	KubeletScheme string // "https" or "http"
	CheckpointDir string // local output/transfer directory; mock default uses OS temp

	// Server Settings
	ServerPort   string
	DebugLogging bool
}

// Validate checks that configured modes, providers, rates, and timeouts are supported.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("configuration is nil")
	}
	if c.MigrationMode != "pod" && c.MigrationMode != "mock" && c.MigrationMode != "statefulset" {
		return fmt.Errorf("unsupported MIGRATION_MODE %q (use pod, mock, or statefulset)", c.MigrationMode)
	}
	if c.MigrationMode == "statefulset" {
		return fmt.Errorf("MIGRATION_MODE=statefulset is unsupported: safe StatefulSet ownership, PVC, and fencing handoff is not implemented")
	}
	if c.CheckpointProvider != "mock" && c.CheckpointProvider != "kubelet" {
		return fmt.Errorf("unsupported CHECKPOINT_PROVIDER %q (use mock or kubelet)", c.CheckpointProvider)
	}
	if c.TransferProvider != "file" && c.TransferProvider != "mock" {
		return fmt.Errorf("unsupported TRANSFER_PROVIDER %q (use file or mock)", c.TransferProvider)
	}
	if c.MigrationMode == "mock" && c.CheckpointProvider != "mock" {
		return fmt.Errorf("MIGRATION_MODE=mock requires CHECKPOINT_PROVIDER=mock")
	}
	if c.MigrationMode == "pod" && c.CheckpointProvider == "mock" {
		return fmt.Errorf("MIGRATION_MODE=pod cannot use CHECKPOINT_PROVIDER=mock; select MIGRATION_MODE=mock for simulation")
	}
	if c.FeasibilityPolicy != "warn" && c.FeasibilityPolicy != "reject" && c.FeasibilityPolicy != "force" {
		return fmt.Errorf("unsupported MIGRATION_FEASIBILITY_POLICY %q (use warn, reject, or force)", c.FeasibilityPolicy)
	}
	if c.ServerPort == "" || c.QueueName == "" {
		return fmt.Errorf("SERVER_PORT and QUEUE_NAME must not be empty")
	}
	if c.KubeletPort < 1 || c.KubeletPort > 65535 {
		return fmt.Errorf("KUBELET_PORT must be between 1 and 65535")
	}
	if c.MaxReplayTime <= 0 || c.MinCutoffTime <= 0 || c.MaxCutoffTime < c.MinCutoffTime || c.MetricsWindow <= 0 {
		return fmt.Errorf("replay time, cutoff bounds, and metrics window must be positive; max cutoff must be >= min cutoff")
	}
	if c.CheckpointTimeout <= 0 || c.TransferTimeout <= 0 || c.RestoreTimeout <= 0 || c.ReplayTimeout <= 0 || c.MigrationTimeout <= 0 {
		return fmt.Errorf("all phase and migration timeouts must be positive")
	}
	return nil
}

// Load reads configuration from environment variables, applying sensible defaults.
func Load() *Config {
	return &Config{
		RabbitMQURL:        getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		QueueName:          getEnv("QUEUE_NAME", "microservices-queue"),
		MaxReplayTime:      getDurationEnv("MAX_REPLAY_TIME", 5*time.Second),
		MinCutoffTime:      getDurationEnv("MIN_CUTOFF_TIME", 1*time.Second),
		MaxCutoffTime:      getDurationEnv("MAX_CUTOFF_TIME", 30*time.Second),
		MetricsWindow:      getDurationEnv("METRICS_WINDOW", 5*time.Second),
		CheckpointTimeout:  getDurationEnv("CHECKPOINT_TIMEOUT", 30*time.Second),
		TransferTimeout:    getDurationEnv("TRANSFER_TIMEOUT", 60*time.Second),
		RestoreTimeout:     getDurationEnv("RESTORE_TIMEOUT", 60*time.Second),
		ReplayTimeout:      getDurationEnv("REPLAY_TIMEOUT", 30*time.Second),
		MigrationTimeout:   getDurationEnv("MIGRATION_TIMEOUT", 5*time.Minute),
		MigrationMode:      strings.ToLower(getEnv("MIGRATION_MODE", "mock")),
		FeasibilityPolicy:  strings.ToLower(getEnv("MIGRATION_FEASIBILITY_POLICY", "warn")),
		CheckpointProvider: strings.ToLower(getEnv("CHECKPOINT_PROVIDER", "mock")),
		TransferProvider:   strings.ToLower(getEnv("TRANSFER_PROVIDER", "mock")),
		KubeletPort:        getIntEnv("KUBELET_PORT", 10250),
		KubeletScheme:      getEnv("KUBELET_SCHEME", "https"),
		CheckpointDir:      getEnv("CHECKPOINT_DIR", filepath.Join(os.TempDir(), "kairokube-checkpoints")),
		ServerPort:         getEnv("SERVER_PORT", "8080"),
		DebugLogging:       getBoolEnv("DEBUG_LOGGING", false),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getDurationEnv(key string, defaultVal time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return defaultVal
	}
	return d
}

func getIntEnv(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}

func getBoolEnv(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return defaultVal
	}
	return b
}
