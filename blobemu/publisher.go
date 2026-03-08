package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// BlobEvent mirrors the Azure Event Grid schema for storage blob events.
type BlobEvent struct {
	Topic           string        `json:"topic"`
	Subject         string        `json:"subject"`
	EventType       string        `json:"eventType"`
	ID              string        `json:"id"`
	DataVersion     string        `json:"dataVersion"`
	MetadataVersion string        `json:"metadataVersion"`
	EventTime       string        `json:"eventTime"`
	Data            BlobEventData `json:"data"`
}

// BlobEventData carries the payload of a blob-storage event.
type BlobEventData struct {
	API                string                 `json:"api"`
	ClientRequestId    string                 `json:"clientRequestId"`
	RequestId          string                 `json:"requestId"`
	ETag               string                 `json:"eTag"`
	ContentType        string                 `json:"contentType"`
	ContentLength      int32                  `json:"contentLength"`
	BlobType           string                 `json:"blobType"`
	URL                string                 `json:"url"`
	Sequencer          string                 `json:"sequencer"`
	StorageDiagnostics StorageDiagnosticsData `json:"storageDiagnostics"`
}

type StorageDiagnosticsData struct {
	BatchId string `json:"batchId"`
}

// Publisher sends blob events to a RabbitMQ exchange.
type Publisher struct {
	conn       *amqp.Connection
	ch         *amqp.Channel
	exchange   string
	routingKey string
	baseURL    string // e.g. "http://blobemu:10000"
}

// PublisherConfig holds RabbitMQ connection settings.
type PublisherConfig struct {
	URL        string // AMQP URL, e.g. "amqp://guest:guest@rabbitmq:5672/"
	Exchange   string // Exchange name (default: "blob-events")
	RoutingKey string // Routing key (default: "blob.created")
	QueueName  string // Queue to declare and bind (default: "blob-events")
	BaseURL    string // Public URL prefix for blob references
}

// NewPublisher connects to RabbitMQ, declares an exchange and queue, and
// returns a ready-to-use Publisher. Pass nil to disable publishing.
func NewPublisher(cfg *PublisherConfig) (*Publisher, error) {
	if cfg == nil || cfg.URL == "" {
		return nil, nil // publishing disabled
	}

	exchange := cfg.Exchange
	if exchange == "" {
		exchange = "blob-events"
	}
	routingKey := cfg.RoutingKey
	if routingKey == "" {
		routingKey = "blob.created"
	}
	queueName := cfg.QueueName
	if queueName == "" {
		queueName = "blob-events"
	}

	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("connecting to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("opening channel: %w", err)
	}

	// Declare a durable topic exchange.
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("declaring exchange: %w", err)
	}

	// Declare and bind the queue so consumers can start receiving immediately.
	q, err := ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("declaring queue: %w", err)
	}
	if err := ch.QueueBind(q.Name, routingKey, exchange, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("binding queue: %w", err)
	}

	slog.Info("RabbitMQ publisher ready", "exchange", exchange, "queue", queueName, "routingKey", routingKey)
	return &Publisher{
		conn:       conn,
		ch:         ch,
		exchange:   exchange,
		routingKey: routingKey,
		baseURL:    cfg.BaseURL,
	}, nil
}

// Close tears down the RabbitMQ connection.
func (p *Publisher) Close() {
	if p == nil {
		return
	}
	p.ch.Close()
	p.conn.Close()
}

// PublishBlobCreated sends an Event-Grid-compatible BlobCreated event.
func (p *Publisher) PublishBlobCreated(container, name, contentType string, size int) error {
	if p == nil {
		return nil // publishing disabled
	}

	evt := BlobEvent{
		Topic:           fmt.Sprintf("/blobServices/default/containers/%s", container),
		Subject:         fmt.Sprintf("/blobServices/default/containers/%s/blobs/%s", container, name),
		EventType:       "Microsoft.Storage.BlobCreated",
		ID:              uuid.New().String(),
		DataVersion:     "1",
		MetadataVersion: "1",
		EventTime:       time.Now().UTC().Format(time.RFC3339),
		Data: BlobEventData{
			API:           "PutBlob",
			ContentType:   contentType,
			ContentLength: int32(size),
			BlobType:      "BlockBlob",
			URL:           fmt.Sprintf("%s/%s/%s", p.baseURL, container, name),
		},
	}

	body, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshalling event: %w", err)
	}

	// Base64-encode to match Azure Storage Queue behaviour.
	// The resize service's ConvertToEvent() expects base64-encoded JSON,
	// because that is what Azure Storage Queues deliver via Dapr.
	encoded := base64.StdEncoding.EncodeToString(body)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = p.ch.PublishWithContext(ctx, p.exchange, p.routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         []byte(encoded),
		DeliveryMode: amqp.Persistent,
	})
	if err != nil {
		return fmt.Errorf("publishing event: %w", err)
	}

	slog.Info("published BlobCreated event", "container", container, "blob", name, "url", evt.Data.URL)
	return nil
}
