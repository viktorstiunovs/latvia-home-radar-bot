package postgres

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/events"
)

func TestBaselineAndNotificationFlow(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatal("refusing to truncate a database whose name does not end in _test")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.pool.Exec(ctx, `TRUNCATE consumed_events,outbox_events,notifications,filter_areas,listing_photos,listings,source_state,filters,source_areas,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	userID, err := store.UpsertUser(ctx, 42, 42, "Tester")
	if err != nil {
		t.Fatal(err)
	}
	activated := time.Now().Add(-time.Minute)
	filterID, err := store.CreateFilter(ctx, domain.SearchFilter{UserID: userID, DealType: domain.DealRent, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, AreaKeys: []string{"lv/riga"}, PriceMax: intPointer(800), Enabled: true, ActivatedAt: &activated})
	if err != nil {
		t.Fatal(err)
	}
	listing := domain.Listing{Source: "test", ExternalID: "one", URL: "https://example.test/one", DealType: domain.DealRent, PropertyType: domain.PropertyApartment, Title: "One", PriceEUR: intPointer(700), AreaKey: "lv/riga/centrs", AreaName: "Centrs", AreaType: "neighbourhood", AreaParentKey: "lv/riga", AreaParentName: "Rīga", DetailsEnriched: true}
	baseline, err := store.ProcessDiscovered(ctx, "test:baseline", []domain.Listing{listing}, false)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Inserted != 1 || baseline.Events != 0 {
		t.Fatalf("unexpected baseline: %+v", baseline)
	}
	listing.ExternalID = "two"
	result, err := store.ProcessDiscovered(ctx, "test:baseline", []domain.Listing{listing}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Inserted != 1 || result.Events != 1 {
		t.Fatalf("unexpected discovery: %+v", result)
	}
	outbox, err := store.PendingOutbox(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox) != 1 || outbox[0].Type != events.ListingDiscoveredV1 {
		t.Fatalf("unexpected outbox: %+v", outbox)
	}
	event, err := events.DecodeListingDiscovered(outbox[0])
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.MatchListingEvent(ctx, outbox[0].ID, event.ListingID, outbox[0].OccurredAt)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected one notification, got %d", created)
	}
	created, err = store.MatchListingEvent(ctx, outbox[0].ID, event.ListingID, outbox[0].OccurredAt)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("duplicate event created %d notifications", created)
	}
	pending, err := store.Pending(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].FilterID != filterID || pending[0].Listing.ExternalID != "two" {
		t.Fatalf("unexpected pending notification: %+v", pending)
	}
}

func intPointer(v int) *int {
	return &v
}
