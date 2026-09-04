package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type OutboxRelay struct {
	store     OutboxStore
	publisher EventPublisher
	interval  time.Duration
	logger    *slog.Logger
}

func NewOutboxRelay(store OutboxStore, publisher EventPublisher, logger *slog.Logger) *OutboxRelay {
	return &OutboxRelay{store: store, publisher: publisher, interval: time.Second, logger: logger}
}

func (r *OutboxRelay) Run(ctx context.Context) error {
	for {
		events, err := r.store.PendingOutbox(ctx, 20)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(r.interval):
			}
			continue
		}
		for _, event := range events {
			if err := r.publisher.Publish(ctx, event); err != nil {
				if retryErr := r.store.RetryOutbox(ctx, event.ID, err); retryErr != nil {
					return retryErr
				}
				return fmt.Errorf("publish outbox event %s: %w", event.ID, err)
			}
			if err := r.store.MarkOutboxPublished(ctx, event.ID); err != nil {
				return err
			}
			r.logger.Info("event published", "event", event.Type, "event_id", event.ID)
		}
	}
}
