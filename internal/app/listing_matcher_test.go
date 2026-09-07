package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/events"
)

type matcherStoreStub struct {
	discoveredCalls int
	priceCalls      int
	priceChange     events.ListingPriceChanged
}

func (s *matcherStoreStub) MatchListingEvent(context.Context, string, int64, time.Time) (int, error) {
	s.discoveredCalls++
	return 1, nil
}

func (s *matcherStoreStub) MatchPriceChangedEvent(_ context.Context, _ string, change events.ListingPriceChanged, _ time.Time) (int, error) {
	s.priceCalls++
	s.priceChange = change
	return 1, nil
}

func TestListingMatcherRoutesPriceChangedEvent(t *testing.T) {
	store := &matcherStoreStub{}
	matcher := NewListingMatcher(store, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	payload, err := events.MarshalListingPriceChanged(42, 7, "ss.lv", "abc", intPointer(900), intPointer(700))
	if err != nil {
		t.Fatal(err)
	}
	event := events.Envelope{ID: "00000000-0000-4000-8000-000000000042", Type: events.ListingPriceChangedV1, OccurredAt: time.Now().UTC(), Data: json.RawMessage(payload)}
	if err := matcher.handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if store.priceCalls != 1 || store.discoveredCalls != 0 || store.priceChange.PriceHistoryID != 7 {
		t.Fatalf("unexpected matcher calls: %+v", store)
	}
}

func TestListingMatcherRejectsUnknownEvent(t *testing.T) {
	store := &matcherStoreStub{}
	matcher := NewListingMatcher(store, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := matcher.handle(context.Background(), events.Envelope{ID: "event", Type: "listing.unknown.v1", OccurredAt: time.Now().UTC()})
	if err == nil || !events.IsPermanent(err) {
		t.Fatalf("expected permanent error, got %v", err)
	}
	if store.priceCalls != 0 || store.discoveredCalls != 0 {
		t.Fatalf("unexpected matcher calls: %+v", store)
	}
}
