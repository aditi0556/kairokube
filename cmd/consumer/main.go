package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/aditi0556/kairokube/pkg/rabbitmq"
)

// State represents the state maintained in the consumer's memory.
//
// This is intentionally stateful because the MS2M experiment needs
// observable in-memory state that can be checkpointed and restored.
type State struct {
	MessageCount int
}

func main() {
	log.Println("Starting Consumer Microservice...")

	// ------------------------------------------------------------
	// Configuration
	// ------------------------------------------------------------

	rabbitURL := getEnv(
		"RABBITMQ_URL",
		"amqp://guest:guest@rabbitmq:5672/",
	)

	queueName := getEnv(
		"QUEUE_NAME",
		"microservices-queue",
	)

	processingDelay := getProcessingDelay()

	log.Printf("RabbitMQ URL: %s", rabbitURL)
	log.Printf("Queue: %s", queueName)
	log.Printf("Processing delay: %s", processingDelay)

	// ------------------------------------------------------------
	// Signal handling
	// ------------------------------------------------------------

	stop := make(chan os.Signal, 1)

	signal.Notify(
		stop,
		os.Interrupt,
		syscall.SIGTERM,
	)

	// ------------------------------------------------------------
	// Connect to RabbitMQ
	// ------------------------------------------------------------

	client, err := rabbitmq.NewClient(rabbitURL)
	if err != nil {
		log.Fatalf(
			"Failed to connect to RabbitMQ: %v",
			err,
		)
	}

	defer func() {
		log.Println("Closing RabbitMQ connection...")

		if err := client.Close(); err != nil {
			log.Printf(
				"Error while closing RabbitMQ client: %v",
				err,
			)
		}
	}()

	log.Println("Connected to RabbitMQ successfully")

	// ------------------------------------------------------------
	// Initialize state
	// ------------------------------------------------------------

	state := &State{
		MessageCount: 0,
	}

	log.Printf(
		"Initial state: MessageCount=%d",
		state.MessageCount,
	)

	// ------------------------------------------------------------
	// Start consuming
	// ------------------------------------------------------------

	messages, err := client.Consume(queueName)
	if err != nil {
		log.Fatalf(
			"Failed to start consuming queue %q: %v",
			queueName,
			err,
		)
	}

	log.Printf(
		"Consumer is ready and waiting for messages from %q",
		queueName,
	)

	// ------------------------------------------------------------
	// Processing loop
	// ------------------------------------------------------------

	for {
		select {

		case <-stop:
			log.Println(
				"Shutdown signal received. Stopping consumer.",
			)

			log.Printf(
				"Final in-memory state: MessageCount=%d",
				state.MessageCount,
			)

			return

		case msg, ok := <-messages:
			if !ok {
				log.Println(
					"RabbitMQ consumer channel closed.",
				)

				log.Printf(
					"Final in-memory state: MessageCount=%d",
					state.MessageCount,
				)

				return
			}

			log.Printf(
				"Received message: %s",
				string(msg.Body),
			)

			// ----------------------------------------------------
			// Simulate message processing time
			// ----------------------------------------------------

			if processingDelay > 0 {
				timer := time.NewTimer(processingDelay)

				<-timer.C
			}

			// ----------------------------------------------------
			// Update application state
			// ----------------------------------------------------

			state.MessageCount++

			log.Printf(
				"Processed message successfully. MessageCount=%d",
				state.MessageCount,
			)

			// ----------------------------------------------------
			// Acknowledge only after processing succeeds
			// ----------------------------------------------------

			if err := msg.Ack(false); err != nil {
				log.Printf(
					"Failed to ACK message: %v",
					err,
				)

				// We deliberately do not silently increment state
				// again or ACK again here.
				//
				// RabbitMQ may redeliver an unacknowledged message.
				continue
			}

			log.Printf(
				"Message acknowledged successfully.",
			)
		}
	}
}

// getEnv returns the environment variable value.
//
// If the variable does not exist, defaultValue is returned.
func getEnv(
	name string,
	defaultValue string,
) string {
	value := os.Getenv(name)

	if value == "" {
		return defaultValue
	}

	return value
}

// getProcessingDelay returns the simulated processing time.
//
// Example:
//
//	PROCESSING_DELAY_MS=50
//
// gives approximately 50 ms of processing time per message.
//
// The paper's baseline uses 50 ms/message, corresponding to a nominal
// processing capacity of approximately 20 messages/sec.
func getProcessingDelay() time.Duration {
	value := os.Getenv("PROCESSING_DELAY_MS")

	if value == "" {
		return 50 * time.Millisecond
	}

	delayMS, err := strconv.Atoi(value)

	if err != nil || delayMS < 0 {
		log.Printf(
			"Invalid PROCESSING_DELAY_MS=%q. Using default 50 ms.",
			value,
		)

		return 50 * time.Millisecond
	}

	return time.Duration(delayMS) * time.Millisecond
}