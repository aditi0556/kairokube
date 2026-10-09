package migration

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// MetricsCollector maintains real-time metrics for MS2M migration observability.
type MetricsCollector struct {
	mu sync.RWMutex

	// Counters
	MigrationTotal          int64
	MigrationSuccessTotal   int64
	MigrationFailureTotal   int64
	MessagesReceivedTotal   int64
	MessagesProcessedTotal  int64
	MessagesReplayedTotal   int64
	DuplicatesDetectedTotal int64

	// Gauges / Timers (last recorded values)
	MigrationDurationSeconds  float64
	MigrationDowntimeSeconds  float64
	CheckpointDurationSeconds float64
	CheckpointSizeBytes       int64
	CheckpointTransferSeconds float64
	RestoreDurationSeconds    float64
	ReplayDurationSeconds     float64
	QueueDepth                int64
	MessageArrivalRate        float64
	MessageProcessingRate     float64
}

// NewMetricsCollector initializes an empty collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{}
}

// IncMigrationTotal increments migration attempts.
func (m *MetricsCollector) IncMigrationTotal() {
	atomic.AddInt64(&m.MigrationTotal, 1)
}

// IncMigrationSuccess increments migration successes.
func (m *MetricsCollector) IncMigrationSuccess() {
	atomic.AddInt64(&m.MigrationSuccessTotal, 1)
}

// IncMigrationFailure increments migration failures.
func (m *MetricsCollector) IncMigrationFailure() {
	atomic.AddInt64(&m.MigrationFailureTotal, 1)
}

// AddMessagesReceived adds to received messages count.
func (m *MetricsCollector) AddMessagesReceived(delta int64) {
	atomic.AddInt64(&m.MessagesReceivedTotal, delta)
}

// AddMessagesProcessed adds to processed messages count.
func (m *MetricsCollector) AddMessagesProcessed(delta int64) {
	atomic.AddInt64(&m.MessagesProcessedTotal, delta)
}

// AddMessagesReplayed adds to replayed messages count.
func (m *MetricsCollector) AddMessagesReplayed(delta int64) {
	atomic.AddInt64(&m.MessagesReplayedTotal, delta)
}

// AddDuplicatesDetected adds to duplicate detections count.
func (m *MetricsCollector) AddDuplicatesDetected(delta int64) {
	atomic.AddInt64(&m.DuplicatesDetectedTotal, delta)
}

// RecordMigrationRun updates gauges from a completed or failed migration record.
func (m *MetricsCollector) RecordMigrationRun(mig *Migration) {
	if mig == nil {
		return
	}
	snapshot := mig.Snapshot()
	m.mu.Lock()
	defer m.mu.Unlock()

	if snapshot.EndTime.After(snapshot.StartTime) {
		m.MigrationDurationSeconds = snapshot.EndTime.Sub(snapshot.StartTime).Seconds()
	}
	m.MigrationDowntimeSeconds = snapshot.Downtime.Seconds()
	m.CheckpointDurationSeconds = snapshot.CheckpointDuration.Seconds()
	m.CheckpointSizeBytes = snapshot.CheckpointSize
	m.CheckpointTransferSeconds = snapshot.TransferDuration.Seconds()
	m.RestoreDurationSeconds = snapshot.RestoreDuration.Seconds()
	m.ReplayDurationSeconds = snapshot.ReplayDuration.Seconds()
	m.MessageArrivalRate = snapshot.Lambda
	m.MessageProcessingRate = snapshot.MuTarget
}

// SetQueueDepth updates current queue depth gauge.
func (m *MetricsCollector) SetQueueDepth(depth int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.QueueDepth = depth
}

// SetRates updates measured arrival and processing rates.
func (m *MetricsCollector) SetRates(lambda, mu float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.MessageArrivalRate = lambda
	m.MessageProcessingRate = mu
}

// RenderPrometheus outputs all metrics formatted for Prometheus scrapers.
func (m *MetricsCollector) RenderPrometheus() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder

	writeMetric := func(name, help, mType, val string) {
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n# TYPE %s %s\n%s %s\n", name, help, name, mType, name, val))
	}

	writeMetric("migration_total", "Total number of migration attempts initiated", "counter", fmt.Sprintf("%d", atomic.LoadInt64(&m.MigrationTotal)))
	writeMetric("migration_success_total", "Total number of migrations completed successfully", "counter", fmt.Sprintf("%d", atomic.LoadInt64(&m.MigrationSuccessTotal)))
	writeMetric("migration_failure_total", "Total number of failed migrations", "counter", fmt.Sprintf("%d", atomic.LoadInt64(&m.MigrationFailureTotal)))
	writeMetric("migration_duration_seconds", "Total duration of the latest migration in seconds", "gauge", fmt.Sprintf("%.4f", m.MigrationDurationSeconds))
	writeMetric("migration_downtime_seconds", "Total service downtime during migration in seconds", "gauge", fmt.Sprintf("%.4f", m.MigrationDowntimeSeconds))
	writeMetric("checkpoint_duration_seconds", "Duration of container checkpoint creation in seconds", "gauge", fmt.Sprintf("%.4f", m.CheckpointDurationSeconds))
	writeMetric("checkpoint_size_bytes", "Size of the checkpoint artifact in bytes", "gauge", fmt.Sprintf("%d", m.CheckpointSizeBytes))
	writeMetric("checkpoint_transfer_seconds", "Duration of checkpoint artifact transfer in seconds", "gauge", fmt.Sprintf("%.4f", m.CheckpointTransferSeconds))
	writeMetric("restore_duration_seconds", "Duration of target restoration in seconds", "gauge", fmt.Sprintf("%.4f", m.RestoreDurationSeconds))
	writeMetric("replay_duration_seconds", "Duration of message replay phase in seconds", "gauge", fmt.Sprintf("%.4f", m.ReplayDurationSeconds))
	writeMetric("messages_received_total", "Total messages received from RabbitMQ", "counter", fmt.Sprintf("%d", atomic.LoadInt64(&m.MessagesReceivedTotal)))
	writeMetric("messages_processed_total", "Total messages processed successfully", "counter", fmt.Sprintf("%d", atomic.LoadInt64(&m.MessagesProcessedTotal)))
	writeMetric("messages_replayed_total", "Total messages replayed during state reconstruction", "counter", fmt.Sprintf("%d", atomic.LoadInt64(&m.MessagesReplayedTotal)))
	writeMetric("duplicates_detected_total", "Total duplicate messages detected and ignored", "counter", fmt.Sprintf("%d", atomic.LoadInt64(&m.DuplicatesDetectedTotal)))
	writeMetric("queue_depth", "Current RabbitMQ queue depth", "gauge", fmt.Sprintf("%d", m.QueueDepth))
	writeMetric("message_arrival_rate", "Measured message arrival rate lambda (msg/s)", "gauge", fmt.Sprintf("%.2f", m.MessageArrivalRate))
	writeMetric("message_processing_rate", "Measured message processing rate mu (msg/s)", "gauge", fmt.Sprintf("%.2f", m.MessageProcessingRate))

	return sb.String()
}
