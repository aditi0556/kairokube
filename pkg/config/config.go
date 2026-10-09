// Package config provides centralized configuration management for the MS2M
// migration manager and related microservices.
package config

import (
	"os"
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
	MigrationMode     string // "pod" or "statefulset"
	FeasibilityPolicy string // "warn", "reject", "force"

	// Providers
	CheckpointProvider string // "kubelet", "mock", "file"
	TransferProvider   string // "file", "mock", "scp"

	// Kubelet FCC Settings
	KubeletPort   int
	KubeletScheme string // "https" or "http"
	CheckpointDir string // default "/var/lib/kubelet/checkpoints"

	// Server Settings
	ServerPort   string
	DebugLogging bool
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
		MigrationMode:      strings.ToLower(getEnv("MIGRATION_MODE", "pod")),
		FeasibilityPolicy:  strings.ToLower(getEnv("MIGRATION_FEASIBILITY_POLICY", "warn")),
		CheckpointProvider: strings.ToLower(getEnv("CHECKPOINT_PROVIDER", "mock")),
		TransferProvider:   strings.ToLower(getEnv("TRANSFER_PROVIDER", "file")),
		KubeletPort:        getIntEnv("KUBELET_PORT", 10250),
		KubeletScheme:      getEnv("KUBELET_SCHEME", "https"),
		CheckpointDir:      getEnv("CHECKPOINT_DIR", "/var/lib/kubelet/checkpoints"),
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
