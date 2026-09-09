package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/clive00lewis/latvia-home-radar/internal/events"
)

type ListingMatcher struct {
	store          ListingMatcherStore
	consumer       EventConsumer
	logger         *slog.Logger
	duplicateAware bool
}

func NewListingMatcher(store ListingMatcherStore, consumer EventConsumer, logger *slog.Logger, duplicateAware ...bool) *ListingMatcher {
	enabled := len(duplicateAware) > 0 && duplicateAware[0]
	return &ListingMatcher{store: store, consumer: consumer, logger: logger, duplicateAware: enabled}
}

func (m *ListingMatcher) Run(ctx context.Context) error {
	return m.consumer.Consume(ctx, m.handle)
}

func (m *ListingMatcher) handle(ctx context.Context, event events.Envelope) error {
	listingID, created, err := m.match(ctx, event)
	if err != nil {
		if events.IsPermanent(err) {
			m.logger.Error("event rejected", "event", event.Type, "event_id", event.ID, "error", err)
			return err
		}
		if events.IsDependencyPending(err) {
			m.logger.Debug("listing event waiting for dependencies", "event", event.Type, "event_id", event.ID, "listing_id", listingID, "reason", err)
			return fmt.Errorf("match listing %d: %w", listingID, err)
		}
		m.logger.Warn("listing event processing failed", "event", event.Type, "event_id", event.ID, "listing_id", listingID, "error", err)
		return fmt.Errorf("match listing %d: %w", listingID, err)
	}
	m.logger.Info("listing event matched", "event", event.Type, "event_id", event.ID, "listing_id", listingID, "notifications", created)
	return nil
}

func (m *ListingMatcher) match(ctx context.Context, event events.Envelope) (int64, int, error) {
	switch event.Type {
	case events.ListingDiscoveredV1:
		data, err := events.DecodeListingDiscovered(event)
		if err != nil {
			return 0, 0, events.Permanent(err)
		}
		var created int
		if m.duplicateAware {
			created, err = m.store.MatchDuplicateAwareListingEvent(ctx, event.ID, data.ListingID, event.OccurredAt)
		} else {
			created, err = m.store.MatchListingEvent(ctx, event.ID, data.ListingID, event.OccurredAt)
		}
		return data.ListingID, created, err
	case events.ListingPriceChangedV1:
		data, err := events.DecodeListingPriceChanged(event)
		if err != nil {
			return 0, 0, events.Permanent(err)
		}
		created, err := m.store.MatchPriceChangedEvent(ctx, event.ID, data, event.OccurredAt)
		return data.ListingID, created, err
	default:
		return 0, 0, events.Permanent(fmt.Errorf("unexpected event type %q", event.Type))
	}
}
