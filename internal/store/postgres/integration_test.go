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
	user, err := store.UpsertUser(ctx, 42, 42, "Tester", "lv")
	if err != nil {
		t.Fatal(err)
	}
	if user.LanguageTag != "lv" {
		t.Fatalf("unexpected initial language: %s", user.LanguageTag)
	}
	if changed, err := store.SetUserLanguage(ctx, 42, "ru"); err != nil || !changed {
		t.Fatalf("set user language: changed=%t err=%v", changed, err)
	}
	user, err = store.UpsertUser(ctx, 42, 42, "Tester", "en")
	if err != nil {
		t.Fatal(err)
	}
	if user.LanguageTag != "ru" {
		t.Fatalf("upsert replaced explicit language: %s", user.LanguageTag)
	}
	defaultUser, err := store.UpsertUser(ctx, 43, 43, "Default", "")
	if err != nil {
		t.Fatal(err)
	}
	if defaultUser.LanguageTag != "en" {
		t.Fatalf("unexpected fallback language: %s", defaultUser.LanguageTag)
	}
	chatIDs, err := store.ListUserChatIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(chatIDs) != 2 || chatIDs[0] != 42 || chatIDs[1] != 43 {
		t.Fatalf("unexpected user chat IDs: %v", chatIDs)
	}
	activated := time.Now().Add(-time.Minute)
	filterID, err := store.CreateFilter(ctx, domain.SearchFilter{UserID: user.ID, DealType: domain.DealRent, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, AreaKeys: []string{"lv/riga"}, PriceMax: intPointer(800), Enabled: true, ActivatedAt: &activated})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := store.SetUserLanguage(ctx, 42, "lv"); err != nil || !changed {
		t.Fatalf("change user language with alert: changed=%t err=%v", changed, err)
	}
	filters, err := store.ListFilters(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(filters) != 1 || filters[0].ID != filterID {
		t.Fatalf("language change modified alerts: %+v", filters)
	}
	listing := domain.Listing{Source: "test", ExternalID: "one", URL: "https://example.test/one", DealType: domain.DealRent, PropertyType: domain.PropertyApartment, Title: "One", PriceEUR: intPointer(700), AreaKey: "lv/riga/centrs", AreaName: "Centrs", AreaType: "neighbourhood", AreaParentKey: "lv/riga", AreaParentName: "Rīga", DetailsEnriched: true}
	baseline, err := store.ProcessDiscovered(ctx, "test:baseline", []domain.Listing{listing}, false)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Inserted != 1 || baseline.Events != 0 {
		t.Fatalf("unexpected baseline: %+v", baseline)
	}
	var baselinePrices int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM listing_price_history`).Scan(&baselinePrices); err != nil {
		t.Fatal(err)
	}
	if baselinePrices != 1 {
		t.Fatalf("expected one baseline price observation, got %d", baselinePrices)
	}
	listing.ExternalID = "two"
	result, err := store.ProcessDiscovered(ctx, "test:baseline", []domain.Listing{listing}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Inserted != 1 || result.Events != 1 {
		t.Fatalf("unexpected discovery: %+v", result)
	}
	var discoveredPrices int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM listing_price_history`).Scan(&discoveredPrices); err != nil {
		t.Fatal(err)
	}
	if discoveredPrices != 2 {
		t.Fatalf("expected initial prices for baseline and discovered listings, got %d", discoveredPrices)
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
	if len(pending) != 1 || pending[0].FilterID != filterID || pending[0].Listing.ExternalID != "two" || pending[0].LanguageTag != "lv" {
		t.Fatalf("unexpected pending notification: %+v", pending)
	}
}

func TestPriceHistoryAndPriceChangeNotifications(t *testing.T) {
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
	if _, err := store.pool.Exec(ctx, `TRUNCATE consumed_events,outbox_events,notifications,filter_areas,listing_photos,listing_price_history,listings,source_state,filters,source_areas,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	user, err := store.UpsertUser(ctx, 42, 42, "Tester", "en")
	if err != nil {
		t.Fatal(err)
	}
	activated := time.Now().Add(-time.Minute)
	filterID, err := store.CreateFilter(ctx, domain.SearchFilter{UserID: user.ID, DealType: domain.DealRent, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, AreaKeys: []string{"lv/riga"}, PriceMax: intPointer(800), Enabled: true, ActivatedAt: &activated})
	if err != nil {
		t.Fatal(err)
	}
	secondFilterID, err := store.CreateFilter(ctx, domain.SearchFilter{UserID: user.ID, DealType: domain.DealRent, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, AreaKeys: []string{"lv/riga"}, PriceMin: intPointer(600), PriceMax: intPointer(750), Enabled: true, ActivatedAt: &activated})
	if err != nil {
		t.Fatal(err)
	}
	listing := domain.Listing{Source: "test", ExternalID: "price-one", URL: "https://example.test/price-one", DealType: domain.DealRent, PropertyType: domain.PropertyApartment, Title: "Price one", PriceEUR: intPointer(900), AreaKey: "lv/riga/centrs", AreaName: "Centrs", AreaType: "neighbourhood", AreaParentKey: "lv/riga", AreaParentName: "Rīga", DetailsEnriched: true, PhotoURLs: []string{"https://example.test/photo.jpg"}}
	result, err := store.ProcessDiscovered(ctx, "test:prices", []domain.Listing{listing}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Inserted != 1 || result.Events != 0 {
		t.Fatalf("unexpected baseline result: %+v", result)
	}
	assertPriceState(t, store, "price-one", []any{900}, intPointer(900), 0)

	keys, err := store.EnrichmentKeys(ctx, []domain.Listing{listing})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("unchanged listing requested enrichment: %v", keys)
	}
	listing.PriceEUR = intPointer(1000)
	keys, err = store.EnrichmentKeys(ctx, []domain.Listing{listing})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := keys[listing.Key()]; !ok {
		t.Fatalf("changed listing did not request enrichment: %v", keys)
	}

	listing.PriceEUR = intPointer(900)
	result, err = store.ProcessDiscovered(ctx, "test:prices", []domain.Listing{listing}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Events != 0 {
		t.Fatalf("unchanged price emitted events: %+v", result)
	}
	assertPriceState(t, store, "price-one", []any{900}, intPointer(900), 0)

	listing.PriceEUR = intPointer(1000)
	result, err = store.ProcessDiscovered(ctx, "test:prices", []domain.Listing{listing}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Events != 1 {
		t.Fatalf("increase did not emit one change event: %+v", result)
	}
	increase := latestPriceEvent(t, store)
	increaseData, err := events.DecodeListingPriceChanged(increase)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.MatchPriceChangedEvent(ctx, increase.ID, increaseData, increase.OccurredAt)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("out-of-range increase created %d notifications", created)
	}

	listing.PriceEUR = nil
	result, err = store.ProcessDiscovered(ctx, "test:prices", []domain.Listing{listing}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Events != 1 {
		t.Fatalf("known-to-unknown transition result: %+v", result)
	}
	knownToUnknown := latestPriceEvent(t, store)
	knownToUnknownData, err := events.DecodeListingPriceChanged(knownToUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if knownToUnknownData.PreviousPriceEUR == nil || *knownToUnknownData.PreviousPriceEUR != 1000 || knownToUnknownData.CurrentPriceEUR != nil {
		t.Fatalf("unexpected known-to-unknown event: %+v", knownToUnknownData)
	}
	created, err = store.MatchPriceChangedEvent(ctx, knownToUnknown.ID, knownToUnknownData, knownToUnknown.OccurredAt)
	if err != nil || created != 0 {
		t.Fatalf("known-to-unknown match: created=%d err=%v", created, err)
	}

	result, err = store.ProcessDiscovered(ctx, "test:prices", []domain.Listing{listing}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Events != 0 {
		t.Fatalf("repeated unknown price emitted events: %+v", result)
	}

	listing.PriceEUR = intPointer(900)
	result, err = store.ProcessDiscovered(ctx, "test:prices", []domain.Listing{listing}, true)
	if err != nil {
		t.Fatal(err)
	}
	unknownToKnown := latestPriceEvent(t, store)
	unknownToKnownData, err := events.DecodeListingPriceChanged(unknownToKnown)
	if err != nil {
		t.Fatal(err)
	}
	if unknownToKnownData.PreviousPriceEUR != nil || unknownToKnownData.CurrentPriceEUR == nil || *unknownToKnownData.CurrentPriceEUR != 900 {
		t.Fatalf("unexpected unknown-to-known event: %+v", unknownToKnownData)
	}
	created, err = store.MatchPriceChangedEvent(ctx, unknownToKnown.ID, unknownToKnownData, unknownToKnown.OccurredAt)
	if err != nil || created != 0 {
		t.Fatalf("unknown-to-known match: created=%d err=%v", created, err)
	}

	listing.PriceEUR = intPointer(700)
	result, err = store.ProcessDiscovered(ctx, "test:prices", []domain.Listing{listing}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Events != 1 {
		t.Fatalf("decrease did not emit one event: %+v", result)
	}
	firstDrop := latestPriceEvent(t, store)
	firstDropData, err := events.DecodeListingPriceChanged(firstDrop)
	if err != nil {
		t.Fatal(err)
	}
	created, err = store.MatchPriceChangedEvent(ctx, firstDrop.ID, firstDropData, firstDrop.OccurredAt)
	if err != nil || created != 2 {
		t.Fatalf("filter-boundary price decrease: created=%d err=%v", created, err)
	}
	created, err = store.MatchPriceChangedEvent(ctx, firstDrop.ID, firstDropData, firstDrop.OccurredAt)
	if err != nil || created != 0 {
		t.Fatalf("duplicate price event: created=%d err=%v", created, err)
	}

	listing.PriceEUR = intPointer(650)
	if _, err := store.ProcessDiscovered(ctx, "test:prices", []domain.Listing{listing}, true); err != nil {
		t.Fatal(err)
	}
	secondDrop := latestPriceEvent(t, store)
	secondDropData, err := events.DecodeListingPriceChanged(secondDrop)
	if err != nil {
		t.Fatal(err)
	}
	created, err = store.MatchPriceChangedEvent(ctx, secondDrop.ID, secondDropData, secondDrop.OccurredAt)
	if err != nil || created != 2 {
		t.Fatalf("second price decrease: created=%d err=%v", created, err)
	}

	listing.PriceEUR = intPointer(720)
	if _, err := store.ProcessDiscovered(ctx, "test:prices", []domain.Listing{listing}, true); err != nil {
		t.Fatal(err)
	}
	inRangeIncrease := latestPriceEvent(t, store)
	inRangeIncreaseData, err := events.DecodeListingPriceChanged(inRangeIncrease)
	if err != nil {
		t.Fatal(err)
	}
	created, err = store.MatchPriceChangedEvent(ctx, inRangeIncrease.ID, inRangeIncreaseData, inRangeIncrease.OccurredAt)
	if err != nil || created != 2 {
		t.Fatalf("in-range price increase: created=%d err=%v", created, err)
	}

	assertPriceState(t, store, "price-one", []any{900, 1000, nil, 900, 700, 650, 720}, intPointer(720), 6)
	pending, err := store.Pending(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 6 {
		t.Fatalf("expected six event-scoped notifications, got %+v", pending)
	}
	wantedFilters := map[int64]bool{filterID: true, secondFilterID: true}
	firstDecreaseNotifications, secondDecreaseNotifications, increaseNotifications := 0, 0, 0
	for _, item := range pending {
		if !wantedFilters[item.FilterID] || item.Type != domain.NotificationPriceChanged {
			t.Fatalf("unexpected notification: %+v", item)
		}
		if !item.Listing.DetailsEnriched || len(item.Listing.PhotoURLs) != 1 || item.Listing.PhotoURLs[0] != "https://example.test/photo.jpg" {
			t.Fatalf("notification lost persisted details or photos: %+v", item.Listing)
		}
		switch {
		case *item.PreviousPriceEUR == 900 && *item.CurrentPriceEUR == 700 && *item.Listing.PriceEUR == 700:
			firstDecreaseNotifications++
		case *item.PreviousPriceEUR == 700 && *item.CurrentPriceEUR == 650 && *item.Listing.PriceEUR == 650:
			secondDecreaseNotifications++
		case *item.PreviousPriceEUR == 650 && *item.CurrentPriceEUR == 720 && *item.Listing.PriceEUR == 720:
			increaseNotifications++
		default:
			t.Fatalf("notification lost triggering snapshot: %+v", item)
		}
	}
	if firstDecreaseNotifications != 2 || secondDecreaseNotifications != 2 || increaseNotifications != 2 {
		t.Fatalf("notifications per price event = %d/%d/%d, want 2/2/2", firstDecreaseNotifications, secondDecreaseNotifications, increaseNotifications)
	}
}

func latestPriceEvent(t *testing.T, store *Store) events.Envelope {
	t.Helper()
	var event events.Envelope
	if err := store.pool.QueryRow(context.Background(), `SELECT id::text,event_type,occurred_at,payload FROM outbox_events WHERE event_type=$1 ORDER BY occurred_at DESC,id DESC LIMIT 1`, events.ListingPriceChangedV1).Scan(&event.ID, &event.Type, &event.OccurredAt, &event.Data); err != nil {
		t.Fatal(err)
	}
	return event
}

func assertPriceState(t *testing.T, store *Store, externalID string, expected []any, current *int, expectedEvents int) {
	t.Helper()
	ctx := context.Background()
	var listingID int64
	var actualCurrent *int
	if err := store.pool.QueryRow(ctx, `SELECT id,price_eur FROM listings WHERE external_id=$1`, externalID).Scan(&listingID, &actualCurrent); err != nil {
		t.Fatal(err)
	}
	if !sameNullableInt(actualCurrent, current) {
		t.Fatalf("current price = %v, want %v", actualCurrent, current)
	}
	rows, err := store.pool.Query(ctx, `SELECT price_eur FROM listing_price_history WHERE listing_id=$1 ORDER BY observed_at,id`, listingID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actual []any
	for rows.Next() {
		var price *int
		if err := rows.Scan(&price); err != nil {
			t.Fatal(err)
		}
		if price == nil {
			actual = append(actual, nil)
		} else {
			actual = append(actual, *price)
		}
	}
	if len(actual) != len(expected) {
		t.Fatalf("price history = %v, want %v", actual, expected)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("price history = %v, want %v", actual, expected)
		}
	}
	var eventCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id=$1 AND event_type=$2`, listingID, events.ListingPriceChangedV1).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != expectedEvents {
		t.Fatalf("price event count = %d, want %d", eventCount, expectedEvents)
	}
}

func sameNullableInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func intPointer(v int) *int {
	return &v
}
