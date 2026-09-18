package main

import (
	"log"

	"github.com/aditi0556/kairokube/pkg/rabbitmq"
)

type State struct {
	MessageCount int
}

func main() {
	log.Println("Starting Consumer Microservice...")

	client, err := rabbitmq.NewClient("amqp://guest:guest@rabbitmq:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer client.Close()

	state := &State{MessageCount: 0}

	messages, err := client.Consume("microservices-queue")
	if err != nil {
		log.Fatalf("Failed to start consuming: %v", err)
	}

	for msg := range messages {
		state.MessageCount++
		log.Printf("Received message: %s. Total messages processed: %d", string(msg.Body), state.MessageCount)
	}
}
