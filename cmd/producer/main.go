package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/aditi0556/kairokube/pkg/rabbitmq"
)

func main() {
	log.Println("Starting MS2M Producer Microservice...")
	rabbitURL := getEnv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/")
	queueName := getEnv("QUEUE_NAME", "microservices-queue")
	rateMsgPerSec := getFloatEnv("PUBLISH_RATE_MSG_PER_SEC", 1.0)
	maxMessages := getIntEnv("MAX_MESSAGES", 0)
	migrationAfter := getIntEnv("MIGRATION_TRIGGER_AFTER_MESSAGES", 0)
	migrationQueue := getEnv("MIGRATION_REQUEST_QUEUE", "migration-requests")
	migrationSource := os.Getenv("MIGRATION_SOURCE_POD")
	migrationNamespace := getEnv("MIGRATION_NAMESPACE", "default")
	migrationTarget := os.Getenv("MIGRATION_TARGET_NODE")
	payload := os.Getenv("MESSAGE_PAYLOAD")
	payloadFile := os.Getenv("MESSAGE_FILE")
	var payloads []string
	if payloadFile != "" {
		var err error
		payloads, err = readPayloadFile(payloadFile)
		if err != nil {
			log.Fatalf("Failed to read MESSAGE_FILE: %v", err)
		}
	} else if payload == "" {
		log.Fatal("MESSAGE_PAYLOAD or MESSAGE_FILE must be configured; refusing to publish synthetic test messages")
	}

	log.Printf("Connecting to RabbitMQ at %s", rabbitURL)
	log.Printf("Target Queue: %s", queueName)
	log.Printf("Publish Rate: %.2f msg/sec", rateMsgPerSec)
	client, err := rabbitmq.NewClient(rabbitURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer client.Close()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	interval := time.Duration(float64(time.Second) / rateMsgPerSec)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var sequence int64
	log.Printf("Producer started. Publishing at interval %s...", interval)
	for {
		select {
		case <-stop:
			log.Printf("Shutdown signal received. Producer stopped. Total published: %d", sequence)
			return
		case <-ticker.C:
			sequence++
			messagePayload := payload
			if len(payloads) > 0 {
				messagePayload = payloads[(sequence-1)%int64(len(payloads))]
			}
			msg := rabbitmq.NewMessage(sequence, messagePayload)
			if err := client.PublishMessage(queueName, msg); err != nil {
				log.Printf("Failed to publish message sequence %d (id=%s): %v", sequence, msg.ID, err)
			} else if sequence%20 == 0 || rateMsgPerSec <= 2.0 {
				log.Printf("Published message id=%s seq=%d timestamp=%s", msg.ID, msg.Sequence, msg.Timestamp.Format(time.RFC3339))
			}
			if migrationAfter > 0 && sequence == int64(migrationAfter) {
				if migrationSource == "" || migrationTarget == "" {
					log.Printf("Migration trigger configured but MIGRATION_SOURCE_POD or MIGRATION_TARGET_NODE is empty")
				} else {
					req := rabbitmq.NewMigrationRequest(migrationSource, migrationNamespace, migrationTarget)
					body, _ := req.Encode()
					if err := client.Publish(migrationQueue, string(body)); err != nil {
						log.Printf("Failed to publish migration request: %v", err)
					} else {
						log.Printf("Published migration request id=%s to %s", req.ID, migrationQueue)
					}
				}
			}
			if maxMessages > 0 && sequence >= int64(maxMessages) {
				log.Printf("Reached maximum message count (%d). Producer exiting.", maxMessages)
				return
			}
		}
	}
}

func readPayloadFile(name string) ([]string, error) {
	path, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var payloads []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if json.Valid([]byte(line)) {
			payloads = append(payloads, line)
			continue
		}
		encoded, _ := json.Marshal(line)
		payloads = append(payloads, string(encoded))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(payloads) == 0 {
		return nil, fmt.Errorf("MESSAGE_FILE contains no non-empty lines")
	}
	return payloads, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
func getFloatEnv(key string, defaultVal float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil || f <= 0 {
		return defaultVal
	}
	return f
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
