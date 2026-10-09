// Package rabbitmq provides RabbitMQ helpers, message structures, and duplicate detection
// for the MS2M migration workflow.
package rabbitmq

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Message represents the standard structured message payload used in the MS2M microservice pipeline.
type Message struct {
	ID        string    `json:"id"`
	Sequence  int64     `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	Payload   string    `json:"payload"`
}

// NewMessage constructs a message with a unique UUID, monotonic sequence number, and timestamp.
func NewMessage(sequence int64, payload string) *Message {
	return &Message{
		ID:        uuid.New().String(),
		Sequence:  sequence,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// Encode serializes the message to JSON bytes.
func (m *Message) Encode() ([]byte, error) {
	return json.Marshal(m)
}

// DecodeMessage deserializes JSON bytes into a Message struct.
func DecodeMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}
	if msg.ID == "" {
		return nil, fmt.Errorf("invalid message: missing id")
	}
	return &msg, nil
}

// DuplicateDetector detects duplicate messages during normal operation or post-migration message replay.
// It keeps a bounded in-memory cache of recently processed message IDs.
type DuplicateDetector struct {
	mu           sync.Mutex
	seenIDs      map[string]time.Time
	maxCapacity  int
	retention    time.Duration
	lastSequence int64
}

// NewDuplicateDetector initializes a detector with max capacity and retention window.
func NewDuplicateDetector(maxCapacity int, retention time.Duration) *DuplicateDetector {
	if maxCapacity <= 0 {
		maxCapacity = 100000
	}
	if retention <= 0 {
		retention = 10 * time.Minute
	}
	return &DuplicateDetector{
		seenIDs:     make(map[string]time.Time, maxCapacity),
		maxCapacity: maxCapacity,
		retention:   retention,
	}
}

// CheckAndRecord checks if a message has been seen before.
// If it is a duplicate, returns true.
// If it is a new message, records its ID and updates the highest sequence number, returning false.
func (d *DuplicateDetector) CheckAndRecord(msg *Message) bool {
	if msg == nil || msg.ID == "" {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if already seen
	if _, exists := d.seenIDs[msg.ID]; exists {
		return true
	}

	// Evict stale entries if approaching capacity
	if len(d.seenIDs) >= d.maxCapacity {
		d.evictStale()
	}

	d.seenIDs[msg.ID] = time.Now()
	if msg.Sequence > d.lastSequence {
		d.lastSequence = msg.Sequence
	}

	return false
}

// LastSequence returns the highest sequence number observed so far.
func (d *DuplicateDetector) LastSequence() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastSequence
}

// Size returns the count of tracked message IDs.
func (d *DuplicateDetector) Size() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.seenIDs)
}

func (d *DuplicateDetector) evictStale() {
	cutoff := time.Now().Add(-d.retention)
	for id, t := range d.seenIDs {
		if t.Before(cutoff) {
			delete(d.seenIDs, id)
		}
	}
}
