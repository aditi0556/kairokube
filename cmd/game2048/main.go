// Command game2048 runs the 2048 board as the stateful workload. It consumes
// move commands from RabbitMQ, applies them deterministically, and exposes the
// board and processing counters on an HTTP status endpoint.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/aditi0556/kairokube/pkg/game2048"
	"github.com/aditi0556/kairokube/pkg/rabbitmq"
)

// gameState is the state captured during forensic checkpointing and rebuilt by
// replaying the message queue on the migration target.
type gameState struct {
	mu                 sync.RWMutex
	Board              *game2048.Board `json:"board"`
	MessagesReceived   int64           `json:"messages_received"`
	MessagesProcessed  int64           `json:"messages_processed"`
	MessagesReplayed   int64           `json:"messages_replayed"`
	DuplicatesDetected int64           `json:"duplicates_detected"`
	LastSequence       int64           `json:"last_sequence"`
	GameOver           bool            `json:"game_over"`
}

func main() {
	log.Println("Starting 2048 stateful game workload...")

	rabbitURL := getEnv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/")
	queueName := getEnv("QUEUE_NAME", "microservices-queue")
	statusPort := getEnv("STATUS_PORT", "8081")
	processingDelay := getProcessingDelay()

	state := &gameState{Board: game2048.NewBoard()}
	detector := rabbitmq.NewDuplicateDetector(100000, 30*time.Minute)

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
	log.Printf("2048 game ready; consuming moves from %q", queueName)

	for {
		select {
		case <-stop:
			log.Println("Shutdown signal received. Stopping game workload.")
			state.mu.RLock()
			log.Printf("Final board: score=%d moves=%d over=%t", state.Board.Score, state.Board.MovesApplied, state.GameOver)
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

			msg, err := rabbitmq.DecodeMessage(delivery.Body)
			if err != nil {
				log.Printf("Rejecting invalid envelope: %v", err)
				_ = delivery.Nack(false, false)
				continue
			}

			// Duplicates are acknowledged without being applied, so a replayed
			// message never double-counts a move.
			if detector.CheckAndRecord(msg) {
				state.mu.Lock()
				state.DuplicatesDetected++
				state.MessagesReplayed++
				state.mu.Unlock()
				_ = delivery.Ack(false)
				continue
			}

			dir, err := game2048.DecodeMove(msg.Payload)
			if err != nil {
				log.Printf("Rejecting invalid move (id=%s seq=%d): %v", msg.ID, msg.Sequence, err)
				_ = delivery.Nack(false, false)
				continue
			}

			if processingDelay > 0 {
				time.Sleep(processingDelay)
			}

			state.mu.Lock()
			changed, applyErr := state.Board.Apply(dir, game2048.RNGForSequence(msg.Sequence))
			if applyErr != nil {
				state.mu.Unlock()
				log.Printf("Failed to apply move seq=%d: %v", msg.Sequence, applyErr)
				_ = delivery.Nack(false, true)
				continue
			}
			state.MessagesProcessed++
			if msg.Sequence > state.LastSequence {
				state.LastSequence = msg.Sequence
			}
			if state.Board.IsOver() {
				state.GameOver = true
			}
			processed := state.MessagesProcessed
			score := state.Board.Score
			state.mu.Unlock()

			if processed%20 == 0 || !changed {
				log.Printf("Move %s seq=%d changed=%t processed=%d score=%d", dir, msg.Sequence, changed, processed, score)
			}
			_ = delivery.Ack(false)
		}
	}
}

func startStatusServer(port string, state *gameState) {
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
	server := &http.Server{Addr: ":" + port, Handler: mux}
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
		return 0
	}
	delayMS, err := strconv.Atoi(value)
	if err != nil || delayMS < 0 {
		return 0
	}
	return time.Duration(delayMS) * time.Millisecond
}
