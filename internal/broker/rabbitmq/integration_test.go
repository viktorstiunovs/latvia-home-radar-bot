package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/events"
)

func TestPublishAndConsume(t *testing.T) {
	url := os.Getenv("TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("TEST_RABBITMQ_URL is not set")
	}
	broker, err := Open(context.Background(), url, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	received := make(chan events.Envelope, 1)
	consumerResult := make(chan error, 1)
	go func() {
		consumerResult <- broker.Consume(ctx, func(_ context.Context, event events.Envelope) error {
			received <- event
			return nil
		})
	}()
	payload, err := events.MarshalListingDiscovered(42, "test", "listing-42")
	if err != nil {
		t.Fatal(err)
	}
	event := events.Envelope{
		ID:         "00000000-0000-4000-8000-000000000042",
		Type:       events.ListingDiscoveredV1,
		OccurredAt: time.Now().UTC(),
		Data:       json.RawMessage(payload),
	}
	if err := broker.Publish(ctx, event); err != nil {
		t.Fatal(err)
	}
	select {
	case actual := <-received:
		if actual.ID != event.ID || actual.Type != event.Type {
			t.Fatalf("unexpected event: %+v", actual)
		}
		cancel()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := <-consumerResult; err != nil && err != context.Canceled {
		t.Fatal(err)
	}
}

func TestTransientFailureUsesDelayedRetry(t *testing.T) {
	url := os.Getenv("TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("TEST_RABBITMQ_URL is not set")
	}
	broker, err := Open(context.Background(), url, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	attempts := make(chan time.Time, 2)
	consumerResult := make(chan error, 1)
	go func() {
		attempt := 0
		consumerResult <- broker.Consume(ctx, func(_ context.Context, event events.Envelope) error {
			if event.ID != "00000000-0000-4000-8000-000000000043" {
				return nil
			}
			attempt++
			attempts <- time.Now()
			if attempt == 1 {
				return errors.New("transient test failure")
			}
			cancel()
			return nil
		})
	}()
	payload, err := events.MarshalListingDiscovered(43, "test", "listing-43")
	if err != nil {
		t.Fatal(err)
	}
	event := events.Envelope{
		ID:         "00000000-0000-4000-8000-000000000043",
		Type:       events.ListingDiscoveredV1,
		OccurredAt: time.Now().UTC(),
		Data:       json.RawMessage(payload),
	}
	if err := broker.Publish(ctx, event); err != nil {
		t.Fatal(err)
	}

	var first, second time.Time
	select {
	case first = <-attempts:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case second = <-attempts:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if elapsed := second.Sub(first); elapsed < eventRetryDelay-500*time.Millisecond {
		t.Fatalf("retry elapsed after %s, want at least %s", elapsed, eventRetryDelay-500*time.Millisecond)
	}
	if err := <-consumerResult; err != nil && err != context.Canceled {
		t.Fatal(err)
	}
}
