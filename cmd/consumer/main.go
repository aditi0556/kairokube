package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/aditi0556/kairokube/pkg/rabbitmq"
)

// State represents observable in-memory state that is captured during forensic checkpointing
// and reconstructed via message replay during migration.
type State struct {
	mu                  sync.RWMutex
	MessageCount        int    `json:"message_count"`
	LastSequence        int64  `json:"last_sequence"`
	LastProcessedHash   string `json:"last_processed_hash"`
	TotalBytesProcessed int64  `json:"total_bytes_processed"`
	MessagesReceived    int64  `json:"messages_received"`
	MessagesProcessed   int64  `json:"messages_processed"`
	MessagesReplayed    int64  `json:"messages_replayed"`
	DuplicatesDetected  int64  `json:"duplicates_detected"`
}

// main consumes application messages with manual acknowledgement. Duplicates are
// acknowledged without being counted again, invalid envelopes are rejected, and
// the state is served on /state and /health. It shuts down on SIGINT/SIGTERM.
func main() {
	log.Println("Starting MS2M Consumer Microservice...")

	rabbitURL := getEnv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/")
	queueName := getEnv("QUEUE_NAME", "microservices-queue")
	statusPort := getEnv("STATUS_PORT", "8081")

	log.Printf("RabbitMQ URL: %s", rabbitURL)
	log.Printf("Queue: %s", queueName)

	state := &State{}
	detector := rabbitmq.NewDuplicateDetector(100000, 30*time.Minute)

	// Start internal status server for health probes and migration manager state queries
	go startStatusServer(statusPort, state)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	client, err := rabbitmq.NewClient(rabbitURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer client.Close()

	messages, err := client.Consume(queueName)
	if err != nil {
		log.Fatalf("Failed to start consuming queue %q: %v", queueName, err)
	}

	log.Printf("Consumer is ready and waiting for messages on %q", queueName)

	for {
		select {
		case <-stop:
			log.Println("Shutdown signal received. Stopping consumer.")
			state.mu.RLock()
			log.Printf("Final State: MessageCount=%d LastSeq=%d Received=%d Processed=%d Duplicates=%d",
				state.MessageCount, state.LastSequence, state.MessagesReceived, state.MessagesProcessed, state.DuplicatesDetected)
			state.mu.RUnlock()
			return

		case delivery, ok := <-messages:
			if !ok {
				log.Println("RabbitMQ delivery channel closed.")
				return
			}

			state.mu.Lock()
			state.MessagesReceived++
			state.mu.Unlock()

			// Decode only the application message schema. Invalid deliveries are rejected without inventing IDs or state.
			msg, err := rabbitmq.DecodeMessage(delivery.Body)
			if err != nil {
				log.Printf("Rejecting invalid message: %v", err)
				_ = delivery.Nack(false, false)
				continue
			}

			// Duplicate detection for message replay synchronization
			if detector.CheckAndRecord(msg) {
				state.mu.Lock()
				state.DuplicatesDetected++
				state.MessagesReplayed++
				state.mu.Unlock()

				log.Printf("Duplicate message detected (id=%s, seq=%d); skipping state update", msg.ID, msg.Sequence)
				_ = delivery.Ack(false)
				continue
			}

			// Execute actual message processing: parse and verify payload, compute cryptographic hash
			payloadBytes := []byte(msg.Payload)
			hash := sha256.Sum256(append(delivery.Body, []byte(msg.ID)...))
			hashHex := hex.EncodeToString(hash[:])

			// Update in-memory application state with computed metrics
			state.mu.Lock()
			state.MessageCount++
			if msg.Sequence > state.LastSequence {
				state.LastSequence = msg.Sequence
			}
			state.LastProcessedHash = hashHex
			state.TotalBytesProcessed += int64(len(payloadBytes))
			state.MessagesProcessed++
			currCount := state.MessageCount
			currSeq := state.LastSequence
			state.mu.Unlock()

			if currCount%20 == 0 || currCount == 1 {
				log.Printf("Processed message id=%s seq=%d count=%d bytes=%d hash=%.8s...", msg.ID, currSeq, currCount, len(payloadBytes), hashHex)
			}

			// Acknowledge message only after processing succeeds
			if err := delivery.Ack(false); err != nil {
				log.Printf("Failed to ACK message: %v", err)
			}
		}
	}
}

// startStatusServer serves /health and /state on port. It runs until the HTTP
// server fails; the error is discarded because the process keeps consuming.
func startStatusServer(port string, state *State) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	})

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}
	_ = server.ListenAndServe()
}

// getEnv returns the environment variable name, or defaultValue when it is unset or empty.
func getEnv(name, defaultValue string) string {
	if val := os.Getenv(name); val != "" {
		return val
	}
	return defaultValue
}
