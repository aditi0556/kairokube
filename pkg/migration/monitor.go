package migration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/aditi0556/kairokube/pkg/rabbitmq"
)

type rateSample struct {
	timestamp time.Time
	count     int64
}

// RealTimeRateMonitor tracks dynamic arrival (lambda) and processing (mu) rates over a sliding window.
type RealTimeRateMonitor struct {
	mu              sync.Mutex
	window          time.Duration
	rmqClient       *rabbitmq.Client
	arrivalLog      []rateSample
	processLog      []rateSample
	defaultMu       float64
	consumerURL     string
	lastProcessed   int64
	lastProcessedAt time.Time
	lastQueueLen    int64
}

// NewRateMonitor creates a new RealTimeRateMonitor.
func NewRateMonitor(rmqClient *rabbitmq.Client, window time.Duration, defaultMu float64) *RealTimeRateMonitor {
	if window <= 0 {
		window = 5 * time.Second
	}
	return &RealTimeRateMonitor{
		window:      window,
		rmqClient:   rmqClient,
		defaultMu:   defaultMu,
		consumerURL: os.Getenv("CONSUMER_STATUS_URL"),
		arrivalLog:  make([]rateSample, 0),
		processLog:  make([]rateSample, 0),
	}
}

// RecordArrival logs newly arrived messages.
func (m *RealTimeRateMonitor) RecordArrival(count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.arrivalLog = append(m.arrivalLog, rateSample{timestamp: time.Now(), count: count})
	m.prune()
}

// RecordProcessing logs completed messages by the target or source.
func (m *RealTimeRateMonitor) RecordProcessing(count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processLog = append(m.processLog, rateSample{timestamp: time.Now(), count: count})
	m.prune()
}

// MeasureRates computes current lambda (arrival rate), mu (processing rate), and queue depth.
func (m *RealTimeRateMonitor) MeasureRates(ctx context.Context, queueName string) (lambda float64, mu float64, queueDepth int64, err error) {
	m.mu.Lock()
	m.prune()
	now := time.Now()
	cutoff := now.Add(-m.window)

	var totalArrivals int64
	for _, s := range m.arrivalLog {
		if s.timestamp.After(cutoff) {
			totalArrivals += s.count
		}
	}

	var totalProcessed int64
	for _, s := range m.processLog {
		if s.timestamp.After(cutoff) {
			totalProcessed += s.count
		}
	}
	m.mu.Unlock()

	windowSec := m.window.Seconds()
	if windowSec <= 0 {
		windowSec = 1.0
	}

	lambda = float64(totalArrivals) / windowSec
	mu = float64(totalProcessed) / windowSec

	// Prefer the live consumer status endpoint when configured.
	if m.consumerURL != "" {
		var status struct {
			MessagesProcessed int64 `json:"messages_processed"`
		}
		if resp, reqErr := http.Get(m.consumerURL); reqErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&status) == nil {
				now := time.Now()
				m.mu.Lock()
				if !m.lastProcessedAt.IsZero() && status.MessagesProcessed >= m.lastProcessed {
					delta := now.Sub(m.lastProcessedAt).Seconds()
					if delta > 0 {
						mu = float64(status.MessagesProcessed-m.lastProcessed) / delta
					}
				}
				m.lastProcessed, m.lastProcessedAt = status.MessagesProcessed, now
				m.mu.Unlock()
			}
		}
	}
	// A configured baseline is only meaningful when explicitly supplied for a mock run.
	if mu <= 0.001 && m.defaultMu > 0 {
		mu = m.defaultMu
	}

	// Query live queue depth from RabbitMQ if client available
	if m.rmqClient != nil && queueName != "" {
		messages, _, qErr := m.rmqClient.InspectQueue(queueName)
		if qErr == nil {
			queueDepth = int64(messages)
			m.mu.Lock()
			m.lastQueueLen = queueDepth
			m.mu.Unlock()
		} else {
			m.mu.Lock()
			queueDepth = m.lastQueueLen
			m.mu.Unlock()
		}
	}

	return lambda, mu, queueDepth, nil
}

func (m *RealTimeRateMonitor) prune() {
	cutoff := time.Now().Add(-2 * m.window)

	cleanArrival := m.arrivalLog[:0]
	for _, s := range m.arrivalLog {
		if s.timestamp.After(cutoff) {
			cleanArrival = append(cleanArrival, s)
		}
	}
	m.arrivalLog = cleanArrival

	cleanProcess := m.processLog[:0]
	for _, s := range m.processLog {
		if s.timestamp.After(cutoff) {
			cleanProcess = append(cleanProcess, s)
		}
	}
	m.processLog = cleanProcess
}
