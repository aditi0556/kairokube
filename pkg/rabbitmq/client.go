// Package rabbitmq provides a small wrapper around the RabbitMQ AMQP client.
//
// The package is used by the producer and consumer microservices to establish
// RabbitMQ connections, publish messages, and consume messages from durable
// queues.
//
// Consumer acknowledgements are handled manually so that a message is not
// considered successfully processed until the consumer explicitly
// acknowledges it. This is important for the stateful microservice migration
// workflow, where unacknowledged messages may need to be redelivered after a
// consumer is stopped or migrated.
package rabbitmq

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Client represents a connection to a RabbitMQ broker.
//
// Client maintains a single AMQP connection and channel and provides helper
// methods for publishing and consuming messages.
//
// A Client should be created with NewClient and closed with Close when it is
// no longer needed.
type Client struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

// NewClient establishes a connection to the RabbitMQ broker specified by url
// and creates an AMQP channel for communication.
//
// The URL must use the AMQP connection-string format, for example:
//
//	amqp://guest:guest@rabbitmq:5672/
//
// The returned Client owns the connection and channel. The caller should
// invoke Close when the client is no longer required.
//
// NewClient returns an error if the URL is empty, the connection cannot be
// established, or the AMQP channel cannot be created.
func NewClient(url string) (*Client, error) {
	if url == "" {
		return nil, fmt.Errorf("RabbitMQ URL cannot be empty")
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to connect to RabbitMQ: %w",
			err,
		)
	}

	ch, err := conn.Channel()
	if err != nil {
		// Close the connection because channel creation failed and the
		// connection is no longer useful.
		_ = conn.Close()

		return nil, fmt.Errorf(
			"failed to open RabbitMQ channel: %w",
			err,
		)
	}

	return &Client{
		conn:    conn,
		channel: ch,
	}, nil
}

// Close releases the AMQP channel and RabbitMQ connection owned by the Client.
//
// Close is safe to call on a nil Client or when the channel or connection has
// already been closed. Errors during shutdown are intentionally ignored
// because the Client's primary purpose is to release resources rather than
// report errors from an already-terminated connection.
func (c *Client) Close() {
	if c == nil {
		return
	}

	if c.channel != nil {
		_ = c.channel.Close()
	}

	if c.conn != nil {
		_ = c.conn.Close()
	}
}

// declareQueue declares a durable RabbitMQ queue with the supplied name.
//
// The queue is configured as:
//
//   - durable: true
//   - autoDelete: false
//   - exclusive: false
//
// A durable queue remains available after a RabbitMQ broker restart, while
// autoDelete=false prevents the queue from being removed when the consumer
// disconnects.
//
// All producers and consumers using the same queue name must use compatible
// queue declaration properties.
func (c *Client) declareQueue(queueName string) (amqp.Queue, error) {
	if c == nil || c.channel == nil {
		return amqp.Queue{}, fmt.Errorf(
			"RabbitMQ client is not initialized",
		)
	}

	if queueName == "" {
		return amqp.Queue{}, fmt.Errorf(
			"queue name cannot be empty",
		)
	}

	queue, err := c.channel.QueueDeclare(
		queueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,   // arguments
	)
	if err != nil {
		return amqp.Queue{}, fmt.Errorf(
			"failed to declare queue %q: %w",
			queueName,
			err,
		)
	}

	return queue, nil
}

// Publish sends a text message to the specified RabbitMQ queue.
//
// The destination queue is declared as a durable queue if it does not already
// exist. Published messages use DeliveryModePersistent so that RabbitMQ can
// persist them when using a durable queue.
//
// Publish returns an error if the client is not initialized, the queue cannot
// be declared, or RabbitMQ rejects the published message.
func (c *Client) Publish(queueName, body string) error {
	if c == nil || c.channel == nil {
		return fmt.Errorf(
			"RabbitMQ client is not initialized",
		)
	}

	queue, err := c.declareQueue(queueName)
	if err != nil {
		return err
	}

	err = c.channel.Publish(
		"",
		queue.Name,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "text/plain",
			DeliveryMode: amqp.Persistent,
			Body:         []byte(body),
		},
	)
	if err != nil {
		return fmt.Errorf(
			"failed to publish message to queue %q: %w",
			queueName,
			err,
		)
	}

	return nil
}

// Consume starts a consumer for the specified RabbitMQ queue.
//
// Messages are consumed with manual acknowledgement:
//
//	autoAck = false
//
// The consumer must explicitly call Delivery.Ack after successfully
// processing a message. If a message is delivered but the consumer
// terminates before acknowledging it, RabbitMQ can make the message
// available for redelivery.
//
// The consumer uses a prefetch count of 1, which limits the number of
// unacknowledged messages delivered to this consumer at one time. This makes
// message processing more predictable for the migration experiment.
//
// Consume returns a receive-only Go channel containing RabbitMQ deliveries.
//
// The caller is responsible for acknowledging each successfully processed
// message using:
//
//	msg.Ack(false)
func (c *Client) Consume(queueName string) (<-chan amqp.Delivery, error) {
	if c == nil || c.channel == nil {
		return nil, fmt.Errorf(
			"RabbitMQ client is not initialized",
		)
	}

	queue, err := c.declareQueue(queueName)
	if err != nil {
		return nil, err
	}

	// Limit this consumer to one unacknowledged message at a time.
	//
	// This keeps message processing controlled and prevents a large number
	// of messages from being in-flight during normal operation or migration.
	err = c.channel.Qos(
		1,     // prefetch count
		0,     // prefetch size
		false, // global
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to configure RabbitMQ QoS: %w",
			err,
		)
	}

	// Disable automatic acknowledgement.
	//
	// The application must explicitly acknowledge a message after it has
	// been successfully processed.
	messages, err := c.channel.Consume(
		queue.Name,
		"",    // consumer name
		false, // autoAck
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,   // arguments
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to start consuming queue %q: %w",
			queueName,
			err,
		)
	}

	return messages, nil
}

// PublishMessage serializes and publishes a structured Message to the queue with persistent delivery.
func (c *Client) PublishMessage(queueName string, msg *Message) error {
	if msg == nil {
		return fmt.Errorf("message cannot be nil")
	}
	body, err := msg.Encode()
	if err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}
	return c.Publish(queueName, string(body))
}

// InspectQueue inspects queue status and returns current message depth and consumer count.
func (c *Client) InspectQueue(queueName string) (messages int, consumers int, err error) {
	if c == nil || c.channel == nil {
		return 0, 0, fmt.Errorf("RabbitMQ client is not initialized")
	}
	q, err := c.channel.QueueInspect(queueName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to inspect queue %q: %w", queueName, err)
	}
	return q.Messages, q.Consumers, nil
}
