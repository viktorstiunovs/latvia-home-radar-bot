package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/events"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	eventsExchange = "home.events"
	deadExchange   = "home.events.dead"
	matcherQueue   = "alert-matcher.v1"
	deadQueue      = "alert-matcher.dead.v1"
	consumerName   = "latvia-home-radar.alert-matcher.v1"
)

type Broker struct {
	connection    *amqp.Connection
	publisher     *amqp.Channel
	consumer      *amqp.Channel
	confirmations <-chan amqp.Confirmation
	publishMu     sync.Mutex
}

func Open(ctx context.Context, url string, attempts int) (*Broker, error) {
	if attempts < 1 {
		return nil, fmt.Errorf("connect attempts must be positive")
	}
	var broker *Broker
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		broker, err = open(url)
		if err == nil {
			return broker, nil
		}
		if attempt+1 < attempts {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	return nil, err
}

func open(url string) (*Broker, error) {
	connection, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	publisher, err := connection.Channel()
	if err != nil {
		connection.Close()
		return nil, fmt.Errorf("open publisher channel: %w", err)
	}
	consumer, err := connection.Channel()
	if err != nil {
		publisher.Close()
		connection.Close()
		return nil, fmt.Errorf("open consumer channel: %w", err)
	}
	broker := &Broker{connection: connection, publisher: publisher, consumer: consumer}
	if err := broker.declareTopology(); err != nil {
		broker.Close()
		return nil, err
	}
	if err := publisher.Confirm(false); err != nil {
		broker.Close()
		return nil, fmt.Errorf("enable publisher confirms: %w", err)
	}
	broker.confirmations = publisher.NotifyPublish(make(chan amqp.Confirmation, 1))
	return broker, nil
}

func (b *Broker) declareTopology() error {
	for _, channel := range []*amqp.Channel{b.publisher, b.consumer} {
		if err := channel.ExchangeDeclare(eventsExchange, "topic", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare events exchange: %w", err)
		}
		if err := channel.ExchangeDeclare(deadExchange, "topic", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare dead-letter exchange: %w", err)
		}
	}
	if _, err := b.consumer.QueueDeclare(deadQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead-letter queue: %w", err)
	}
	if err := b.consumer.QueueBind(deadQueue, "#", deadExchange, false, nil); err != nil {
		return fmt.Errorf("bind dead-letter queue: %w", err)
	}
	arguments := amqp.Table{"x-dead-letter-exchange": deadExchange}
	if _, err := b.consumer.QueueDeclare(matcherQueue, true, false, false, false, arguments); err != nil {
		return fmt.Errorf("declare matcher queue: %w", err)
	}
	if err := b.consumer.QueueBind(matcherQueue, events.ListingDiscoveredV1, eventsExchange, false, nil); err != nil {
		return fmt.Errorf("bind matcher queue: %w", err)
	}
	if err := b.consumer.QueueBind(matcherQueue, events.ListingPriceChangedV1, eventsExchange, false, nil); err != nil {
		return fmt.Errorf("bind price-change events: %w", err)
	}
	if err := b.consumer.Qos(10, 0, false); err != nil {
		return fmt.Errorf("set consumer prefetch: %w", err)
	}
	return nil
}

func (b *Broker) Publish(ctx context.Context, event events.Envelope) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	b.publishMu.Lock()
	defer b.publishMu.Unlock()
	if err := b.publisher.PublishWithContext(ctx, eventsExchange, event.Type, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    event.ID,
		Type:         event.Type,
		Timestamp:    event.OccurredAt,
		Body:         body,
	}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case confirmation, ok := <-b.confirmations:
		if !ok {
			return fmt.Errorf("publisher confirmation channel closed")
		}
		if !confirmation.Ack {
			return fmt.Errorf("RabbitMQ rejected published event")
		}
		return nil
	}
}

func (b *Broker) Consume(ctx context.Context, handler func(context.Context, events.Envelope) error) error {
	deliveries, err := b.consumer.Consume(matcherQueue, consumerName, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("start RabbitMQ consumer: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("RabbitMQ delivery channel closed")
			}
			var event events.Envelope
			if err := json.Unmarshal(delivery.Body, &event); err != nil || event.ID == "" || event.Type == "" || event.OccurredAt.IsZero() {
				if rejectErr := delivery.Reject(false); rejectErr != nil {
					return rejectErr
				}
				continue
			}
			if err := handler(ctx, event); err != nil {
				if events.IsPermanent(err) {
					if rejectErr := delivery.Reject(false); rejectErr != nil {
						return rejectErr
					}
					continue
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
				}
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					return nackErr
				}
				continue
			}
			if err := delivery.Ack(false); err != nil {
				return err
			}
		}
	}
}

func (b *Broker) Close() error {
	if b.consumer != nil {
		_ = b.consumer.Close()
	}
	if b.publisher != nil {
		_ = b.publisher.Close()
	}
	if b.connection != nil {
		return b.connection.Close()
	}
	return nil
}
