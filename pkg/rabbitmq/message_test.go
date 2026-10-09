package rabbitmq

import (
	"strings"
	"testing"
	"time"
)

func TestMessageEncodingDecoding(t *testing.T) {
	orig := NewMessage(42, "stateful-telemetry-payload")
	if orig.ID == "" {
		t.Fatal("expected non-empty message ID")
	}
	if orig.Sequence != 42 {
		t.Fatalf("expected sequence 42, got %d", orig.Sequence)
	}

	encoded, err := orig.Encode()
	if err != nil {
		t.Fatalf("failed to encode message: %v", err)
	}

	decoded, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("failed to decode message: %v", err)
	}

	if decoded.ID != orig.ID {
		t.Errorf("ID mismatch: %s vs %s", decoded.ID, orig.ID)
	}
	if decoded.Sequence != orig.Sequence {
		t.Errorf("Sequence mismatch: %d vs %d", decoded.Sequence, orig.Sequence)
	}
	if decoded.Payload != orig.Payload {
		t.Errorf("Payload mismatch: %s vs %s", decoded.Payload, orig.Payload)
	}
}

func TestMessageSchemaVersionValidation(t *testing.T) {
	msg := NewMessage(7, "payload")
	if msg.Version != 1 {
		t.Fatalf("expected schema version 1, got %d", msg.Version)
	}
	encoded, err := msg.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeMessage(encoded)
	if err != nil || decoded.Version != 1 {
		t.Fatalf("decode = %#v, %v", decoded, err)
	}
	if _, err := DecodeMessage([]byte(`{"version":99,"id":"x"}`)); err == nil || !strings.Contains(err.Error(), "unsupported message schema version") {
		t.Fatalf("expected unsupported schema version error, got %v", err)
	}
}

func TestDuplicateDetector(t *testing.T) {
	detector := NewDuplicateDetector(1000, 5*time.Minute)

	msg1 := NewMessage(1, "msg-1")
	msg2 := NewMessage(2, "msg-2")

	// First presentation should not be duplicate
	if detector.CheckAndRecord(msg1) {
		t.Errorf("first presentation of msg1 reported as duplicate")
	}
	if detector.CheckAndRecord(msg2) {
		t.Errorf("first presentation of msg2 reported as duplicate")
	}

	// Second presentation of msg1 must be flagged as duplicate
	if !detector.CheckAndRecord(msg1) {
		t.Errorf("second presentation of msg1 failed to be flagged as duplicate")
	}

	if detector.LastSequence() != 2 {
		t.Errorf("expected LastSequence=2, got %d", detector.LastSequence())
	}
	if detector.Size() != 2 {
		t.Errorf("expected Size=2, got %d", detector.Size())
	}
}
