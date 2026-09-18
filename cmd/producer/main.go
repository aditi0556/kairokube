package main

import (
	"log"
	"time"

	"github.com/aditi0556/kairokube/pkg/rabbitmq"
)

func main() {
	log.Println("Starting Producer Microservice...")

	client, err := rabbitmq.NewClient("amqp://guest:guest@rabbitmq:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer client.Close()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		err := client.Publish("microservices-queue", "Hello from Producer!")
		if err != nil {
			log.Printf("Failed to publish message: %v", err)
		} else {
			log.Println("Message published successfully.")
		}
	}
}
