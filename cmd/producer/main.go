package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
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

	var sequence int64 = 0
	log.Printf("Producer started. Publishing at interval %s...", interval)

	for {
		select {
		case <-stop:
			log.Printf("Shutdown signal received. Producer stopped. Total published: %d", sequence)
			return

		case <-ticker.C:
			sequence++
			payload := fmt.Sprintf("MS2M-Telemetry-Payload-#%d", sequence)
			msg := rabbitmq.NewMessage(sequence, payload)

			err := client.PublishMessage(queueName, msg)
			if err != nil {
				log.Printf("Failed to publish message sequence %d (id=%s): %v", sequence, msg.ID, err)
			} else {
				if sequence%20 == 0 || rateMsgPerSec <= 2.0 {
					log.Printf("Published message id=%s seq=%d timestamp=%s", msg.ID, msg.Sequence, msg.Timestamp.Format(time.RFC3339))
				}
			}

			if maxMessages > 0 && sequence >= int64(maxMessages) {
				log.Printf("Reached maximum message count (%d). Producer exiting.", maxMessages)
				return
			}
		}
	}
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
