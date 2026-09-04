package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/clive00lewis/latvia-home-radar/internal/events"
)

type ListingMatcher struct {
	store    ListingMatcherStore
	consumer EventConsumer
	logger   *slog.Logger
}

func NewListingMatcher(store ListingMatcherStore, consumer EventConsumer, logger *slog.Logger) *ListingMatcher {
	return &ListingMatcher{store: store, consumer: consumer, logger: logger}
}

func (m *ListingMatcher) Run(ctx context.Context) error {
	return m.consumer.Consume(ctx, m.handle)
}

func (m *ListingMatcher) handle(ctx context.Context, event events.Envelope) error {
	data, err := events.DecodeListingDiscovered(event)
	if err != nil {
		m.logger.Error("event rejected", "event", event.Type, "event_id", event.ID, "error", err)
		return events.Permanent(err)
	}
	created, err := m.store.MatchListingEvent(ctx, event.ID, data.ListingID, event.OccurredAt)
	if err != nil {
		m.logger.Warn("listing event processing failed", "event", event.Type, "event_id", event.ID, "listing_id", data.ListingID, "error", err)
		return fmt.Errorf("match listing %d: %w", data.ListingID, err)
	}
	m.logger.Info("listing event matched", "event", event.Type, "event_id", event.ID, "listing_id", data.ListingID, "notifications", created)
	return nil
}
