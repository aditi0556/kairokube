package rabbitmq

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

// Client is a wrapper around the standard RabbitMQ connection and channel.
// It simplifies the process of connecting, publishing, and consuming messages.
type Client struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

// NewClient establishes a connection to a RabbitMQ server and opens a channel.
// The URL should be in the format: amqp://user:password@host:port/
func NewClient(url string) (*Client, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close() // Ensure connection is closed if channel creation fails
		return nil, err
	}

	return &Client{
		conn:    conn,
		channel: ch,
	}, nil
}

// Close gracefully shuts down the channel and connection to RabbitMQ.
func (c *Client) Close() {
	if c.channel != nil {
		c.channel.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}

// Publish sends a simple text message to the specified queue.
// It automatically declares the queue to ensure it exists before sending.
func (c *Client) Publish(queueName, body string) error {
	// Declare a standard, non-durable queue
	q, err := c.channel.QueueDeclare(
		queueName, false, false, false, false, nil,
	)
	if err != nil {
		return err
	}

	// Publish the message to the queue
	err = c.channel.Publish(
		"", q.Name, false, false,
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        []byte(body),
		},
	)
	return err
}

// Consume starts a listener on the specified queue and returns a Go channel
// that will receive incoming messages.
func (c *Client) Consume(queueName string) (<-chan amqp.Delivery, error) {
	// Declare the queue to ensure it exists
	q, err := c.channel.QueueDeclare(
		queueName, false, false, false, false, nil,
	)
	if err != nil {
		return nil, err
	}

	// Start consuming messages with auto-acknowledgement enabled (autoAck=true)
	msgs, err := c.channel.Consume(
		q.Name, "", true, false, false, false, nil,
	)
	if err != nil {
		return nil, err
	}

	return msgs, nil
}
