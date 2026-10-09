package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/aditi0556/kairokube/pkg/rabbitmq"
)

// State represents observable in-memory state that is captured during forensic checkpointing
// and reconstructed via message replay during migration.
type State struct {
	mu                 sync.RWMutex
	MessageCount       int   `json:"message_count"`
	LastSequence       int64 `json:"last_sequence"`
	MessagesReceived   int64 `json:"messages_received"`
	MessagesProcessed  int64 `json:"messages_processed"`
	MessagesReplayed   int64 `json:"messages_replayed"`
	DuplicatesDetected int64 `json:"duplicates_detected"`
}

func main() {
	log.Println("Starting MS2M Consumer Microservice...")

	rabbitURL := getEnv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/")
	queueName := getEnv("QUEUE_NAME", "microservices-queue")
	statusPort := getEnv("STATUS_PORT", "8081")
	processingDelay := getProcessingDelay()

	log.Printf("RabbitMQ URL: %s", rabbitURL)
	log.Printf("Queue: %s", queueName)
	log.Printf("Processing delay: %s", processingDelay)

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

			// Decode message
			msg, err := rabbitmq.DecodeMessage(delivery.Body)
			if err != nil {
				// Fallback for plain text messages
				msg = &rabbitmq.Message{
					ID:        fmt.Sprintf("raw-%d", time.Now().UnixNano()),
					Sequence:  0,
					Timestamp: time.Now().UTC(),
					Payload:   string(delivery.Body),
				}
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

			// Simulate per-message processing latency
			if processingDelay > 0 {
				time.Sleep(processingDelay)
			}

			// Update in-memory application state
			state.mu.Lock()
			state.MessageCount++
			if msg.Sequence > state.LastSequence {
				state.LastSequence = msg.Sequence
			}
			state.MessagesProcessed++
			currCount := state.MessageCount
			currSeq := state.LastSequence
			state.mu.Unlock()

			if currCount%20 == 0 || processingDelay >= 100*time.Millisecond {
				log.Printf("Processed message id=%s seq=%d MessageCount=%d", msg.ID, currSeq, currCount)
			}

			// Acknowledge message only after processing succeeds
			if err := delivery.Ack(false); err != nil {
				log.Printf("Failed to ACK message: %v", err)
			}
		}
	}
}

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

func getEnv(name, defaultValue string) string {
	if val := os.Getenv(name); val != "" {
		return val
	}
	return defaultValue
}

func getProcessingDelay() time.Duration {
	value := os.Getenv("PROCESSING_DELAY_MS")
	if value == "" {
		return 50 * time.Millisecond
	}
	delayMS, err := strconv.Atoi(value)
	if err != nil || delayMS < 0 {
		return 50 * time.Millisecond
	}
	return time.Duration(delayMS) * time.Millisecond
}
