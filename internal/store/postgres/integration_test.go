package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/events"
	"github.com/clive00lewis/latvia-home-radar/internal/identity"
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
	listing.PhotoURLs = []string{"https://example.test/replacement.jpg"}
	keys, err = store.EnrichmentKeys(ctx, []domain.Listing{listing})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := keys[listing.Key()]; !ok {
		t.Fatalf("changed photo input did not request enrichment: %v", keys)
	}
	listing.PhotoURLs = []string{"https://example.test/photo.jpg"}

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
	if err != nil || created != 1 {
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
	if err != nil || created != 1 {
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
	if err != nil || created != 1 {
		t.Fatalf("in-range price increase: created=%d err=%v", created, err)
	}

	assertPriceState(t, store, "price-one", []any{900, 1000, nil, 900, 700, 650, 720}, intPointer(720), 6)
	pending, err := store.Pending(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 3 {
		t.Fatalf("expected three user-coalesced event notifications, got %+v", pending)
	}
	firstDecreaseNotifications, secondDecreaseNotifications, increaseNotifications := 0, 0, 0
	for _, item := range pending {
		if item.FilterID != filterID || item.Type != domain.NotificationPriceChanged {
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
	if firstDecreaseNotifications != 1 || secondDecreaseNotifications != 1 || increaseNotifications != 1 {
		t.Fatalf("notifications per price event = %d/%d/%d, want 1/1/1", firstDecreaseNotifications, secondDecreaseNotifications, increaseNotifications)
	}
	if secondFilterID == filterID {
		t.Fatal("overlapping test filters unexpectedly share an ID")
	}
}

func TestListingSignalJobsRetainImmutableSnapshotsAndRetryAtomically(t *testing.T) {
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
	if _, err := store.pool.Exec(ctx, `TRUNCATE listing_photo_fingerprints,listing_signal_snapshots,listing_signal_attempts,listing_signal_jobs,consumed_events,outbox_events,notifications,filter_areas,listing_photos,listing_price_history,listings,source_state,filters,source_areas,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	listing := domain.Listing{Source: "ss.lv", ExternalID: "signals-one", URL: "https://www.ss.lv/msg/signals-one.html", DealType: domain.DealRent, PropertyType: domain.PropertyApartment, Title: "Concise title", PriceEUR: intPointer(700), Address: "Brīvības iela 48", Rooms: intPointer(2), PhotoURLs: []string{"https://i.ss.lv/gallery/example.800.jpg"}}
	result, err := store.ProcessDiscovered(ctx, "ss.lv:latvia:apartments:rent", []domain.Listing{listing}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Inserted != 1 || result.Events != 0 {
		t.Fatalf("unexpected silent baseline result: %+v", result)
	}
	jobs, err := store.ClaimListingSignalJobs(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Generation != 1 || jobs[0].AttemptID == 0 {
		t.Fatalf("unexpected baseline signal jobs: %+v", jobs)
	}
	job := jobs[0]
	signals := testListingSignals(job.Listing, strings.Repeat("a", 64))
	if err := store.CompleteListingSignalJob(ctx, job, signals); err != nil {
		t.Fatal(err)
	}
	assertSignalState(t, store, job.Listing.ID, 1, 1, "completed")
	var description, normalizedAddress, normalizedDescription string
	if err := store.pool.QueryRow(ctx, `SELECT description,normalized_address,normalized_description FROM listings WHERE id=$1`, job.Listing.ID).Scan(&description, &normalizedAddress, &normalizedDescription); err != nil {
		t.Fatal(err)
	}
	if description != signals.Listing.Description || normalizedAddress != signals.Listing.NormalizedAddress || normalizedDescription != signals.Listing.NormalizedDescription {
		t.Fatalf("current listing signals = %q/%q/%q", description, normalizedAddress, normalizedDescription)
	}

	priceOnly := listing
	priceOnly.PriceEUR = intPointer(750)
	if _, err := store.ProcessDiscovered(ctx, "ss.lv:latvia:apartments:rent", []domain.Listing{priceOnly}, true); err != nil {
		t.Fatal(err)
	}
	jobs, err = store.ClaimListingSignalJobs(ctx, 1)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("price-only update queued signals: jobs=%+v err=%v", jobs, err)
	}
	assertSignalState(t, store, job.Listing.ID, 1, 1, "completed")
	reusable, err := store.ReusablePhotoFingerprints(ctx, job.Listing.ID)
	if err != nil || len(reusable) != 1 || reusable[0].SourceURL != listing.PhotoURLs[0] {
		t.Fatalf("reusable fingerprints=%+v err=%v", reusable, err)
	}

	changed := priceOnly
	changed.Description = "Changed matching description"
	if _, err := store.ProcessDiscovered(ctx, "ss.lv:latvia:apartments:rent", []domain.Listing{changed}, true); err != nil {
		t.Fatal(err)
	}
	jobs, err = store.ClaimListingSignalJobs(ctx, 1)
	if err != nil || len(jobs) != 1 || jobs[0].Generation != 2 {
		t.Fatalf("changed-input claim: jobs=%+v err=%v", jobs, err)
	}
	staleJob := jobs[0]
	changed.Address = "Brīvības iela 50"
	if _, err := store.ProcessDiscovered(ctx, "ss.lv:latvia:apartments:rent", []domain.Listing{changed}, true); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteListingSignalJob(ctx, staleJob, signals); err != nil {
		t.Fatal(err)
	}
	var status string
	var generation int64
	if err := store.pool.QueryRow(ctx, `SELECT status,generation FROM listing_signal_jobs WHERE listing_id=$1`, job.Listing.ID).Scan(&status, &generation); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || generation != 3 {
		t.Fatalf("superseded job state = %s/%d", status, generation)
	}

	jobs, err = store.ClaimListingSignalJobs(ctx, 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("replacement claim: jobs=%+v err=%v", jobs, err)
	}
	badSignals := testListingSignals(jobs[0].Listing, strings.Repeat("b", 64))
	badSignals.Photos[0].ExactHash = "invalid"
	if err := store.CompleteListingSignalJob(ctx, jobs[0], badSignals); err == nil {
		t.Fatal("invalid fingerprint unexpectedly completed")
	}
	assertSignalState(t, store, job.Listing.ID, 1, 3, "processing")
	if err := store.RetryListingSignalJob(ctx, jobs[0], errors.New("invalid test fingerprint")); err != nil {
		t.Fatal(err)
	}
	assertSignalState(t, store, job.Listing.ID, 1, 3, "pending")
	var outcome, lastError string
	if err := store.pool.QueryRow(ctx, `SELECT outcome,COALESCE(error,'') FROM listing_signal_attempts WHERE id=$1`, jobs[0].AttemptID).Scan(&outcome, &lastError); err != nil {
		t.Fatal(err)
	}
	if outcome != "failed" || !strings.Contains(lastError, "invalid test fingerprint") {
		t.Fatalf("failed attempt = %q/%q", outcome, lastError)
	}

	removed := domain.Listing{Source: "city24.lv", ExternalID: "signals-gone", URL: "https://www.city24.lv/real-estate/signals-gone", DealType: domain.DealSale, PropertyType: domain.PropertyApartment, Title: "Removed listing", PriceEUR: intPointer(90000), Address: "Dzirnavu iela 10"}
	if result, err := store.ProcessDiscovered(ctx, "city24.lv:latvia:apartments:sale", []domain.Listing{removed}, false); err != nil || result.Inserted != 1 {
		t.Fatalf("removed listing discovery=%+v err=%v", result, err)
	}
	jobs, err = store.ClaimListingSignalJobs(ctx, 1)
	if err != nil || len(jobs) != 1 || jobs[0].Listing.ExternalID != removed.ExternalID {
		t.Fatalf("removed listing signal job=%+v err=%v", jobs, err)
	}
	removedJob := jobs[0]
	removedJob.Listing.NormalizedAddress = "dzirnavu iela 10"
	removedSignals := domain.ListingSignals{
		Listing:              removedJob.Listing,
		InputHash:            strings.Repeat("e", 64),
		NormalizationVersion: "text-nfkd-v1",
		Availability:         &domain.AvailabilityObservation{Status: domain.AvailabilityInactive, Evidence: "City24 HTTP 410"},
	}
	if err := store.CompleteListingSignalJob(ctx, removedJob, removedSignals); err != nil {
		t.Fatal(err)
	}
	var removedAvailability, removedSignalStatus, removedAttemptOutcome string
	var removedSnapshots, removedFingerprints, removedResolutionJobs int
	if err := store.pool.QueryRow(ctx, `SELECT l.availability_status,j.status,a.outcome,(SELECT count(*) FROM listing_signal_snapshots s WHERE s.listing_id=l.id),(SELECT count(*) FROM listing_photo_fingerprints p WHERE p.listing_id=l.id),(SELECT count(*) FROM duplicate_resolution_jobs r JOIN listing_signal_snapshots s ON s.id=r.snapshot_id WHERE s.listing_id=l.id) FROM listings l JOIN listing_signal_jobs j ON j.listing_id=l.id JOIN listing_signal_attempts a ON a.listing_id=l.id WHERE l.id=$1 ORDER BY a.id DESC LIMIT 1`, removedJob.Listing.ID).Scan(&removedAvailability, &removedSignalStatus, &removedAttemptOutcome, &removedSnapshots, &removedFingerprints, &removedResolutionJobs); err != nil {
		t.Fatal(err)
	}
	if removedAvailability != string(domain.AvailabilityInactive) || removedSignalStatus != "completed" || removedAttemptOutcome != "completed" || removedSnapshots != 1 || removedFingerprints != 0 || removedResolutionJobs != 1 {
		t.Fatalf("removed listing state availability=%s job=%s attempt=%s snapshots=%d fingerprints=%d resolution_jobs=%d", removedAvailability, removedSignalStatus, removedAttemptOutcome, removedSnapshots, removedFingerprints, removedResolutionJobs)
	}
	var observationStatus, observationKind, evidence string
	var transitioned bool
	if err := store.pool.QueryRow(ctx, `SELECT status,observation_kind,evidence,is_transition FROM listing_availability_observations WHERE listing_id=$1 ORDER BY observed_at DESC,id DESC LIMIT 1`, removedJob.Listing.ID).Scan(&observationStatus, &observationKind, &evidence, &transitioned); err != nil {
		t.Fatal(err)
	}
	if observationStatus != string(domain.AvailabilityInactive) || observationKind != "provider_check" || evidence != "City24 HTTP 410" || !transitioned {
		t.Fatalf("removed listing observation=%s/%s/%q transition=%t", observationStatus, observationKind, evidence, transitioned)
	}
}

func TestShadowDuplicateResolutionRetainsDecisionsAndMembershipCorrections(t *testing.T) {
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
	if _, err := store.pool.Exec(ctx, `TRUNCATE duplicate_resolution_attempts,duplicate_resolution_jobs,property_listing_memberships,duplicate_decisions,properties,listing_photo_fingerprints,listing_signal_snapshots,listing_signal_attempts,listing_signal_jobs,consumed_events,outbox_events,notifications,filter_areas,listing_photos,listing_price_history,listings,source_state,filters,source_areas,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	var inspectionView string
	if err := store.pool.QueryRow(ctx, `SELECT COALESCE(to_regclass('duplicate_decision_inspection')::text,'')`).Scan(&inspectionView); err != nil || inspectionView == "" {
		t.Fatalf("inspection view missing: value=%q err=%v", inspectionView, err)
	}

	first := domain.Listing{Source: "ss.lv", ExternalID: "duplicate-one", URL: "https://www.ss.lv/msg/duplicate-one.html", DealType: domain.DealSale, PropertyType: domain.PropertyApartment, Title: "One", PriceEUR: intPointer(120000), Address: "Brīvības iela 48", AreaKey: "lv/riga/centrs", Rooms: intPointer(2), AreaM2: floatPointer(54), Floor: intPointer(3), TotalFloors: intPointer(5), PhotoURLs: []string{"https://i.ss.lv/one.jpg"}}
	firstID, firstProperty := resolveIntegrationListing(t, store, first, strings.Repeat("a", 64), strings.Repeat("c", 64), "0000000000000000")

	second := first
	second.Source = "city24.lv"
	second.ExternalID = "duplicate-two"
	second.URL = "https://www.city24.lv/real-estate/apartments-for-sale/duplicate-two"
	second.PhotoURLs = []string{"https://static.img-city24.lv/two.jpg"}
	secondID, secondProperty := resolveIntegrationListing(t, store, second, strings.Repeat("b", 64), strings.Repeat("c", 64), "0000000000000000")
	if firstProperty != secondProperty {
		t.Fatalf("accepted duplicate properties = %d/%d", firstProperty, secondProperty)
	}

	third := first
	third.ExternalID = "same-building-other-flat"
	third.URL = "https://www.ss.lv/msg/same-building-other-flat.html"
	third.PhotoURLs = []string{"https://i.ss.lv/three.jpg"}
	thirdID, thirdProperty := resolveIntegrationListing(t, store, third, strings.Repeat("d", 64), strings.Repeat("e", 64), "ffffffffffffffff")
	if thirdProperty == firstProperty {
		t.Fatalf("ambiguous listing %d merged into property %d", thirdID, thirdProperty)
	}

	var acceptedDecisionID int64
	var automaticStatus, status string
	if err := store.pool.QueryRow(ctx, `SELECT id,automatic_status,status FROM duplicate_decisions WHERE left_listing_id=least($1::bigint,$2::bigint) AND right_listing_id=greatest($1::bigint,$2::bigint)`, firstID, secondID).Scan(&acceptedDecisionID, &automaticStatus, &status); err != nil {
		t.Fatal(err)
	}
	if automaticStatus != string(domain.DuplicateAccepted) || status != string(domain.DuplicateAccepted) {
		t.Fatalf("duplicate decision = %s/%s", automaticStatus, status)
	}
	var ambiguousCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM duplicate_decisions WHERE (left_listing_id=$1 OR right_listing_id=$1) AND automatic_status='ambiguous'`, thirdID).Scan(&ambiguousCount); err != nil || ambiguousCount == 0 {
		t.Fatalf("ambiguous decisions = %d err=%v", ambiguousCount, err)
	}

	var propertyCount, membershipCount, decisionCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM properties`).Scan(&propertyCount); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM property_listing_memberships`).Scan(&membershipCount); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM duplicate_decisions`).Scan(&decisionCount); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.ClaimDuplicateResolutionJobs(ctx, 1)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("unexpected remaining jobs: %+v err=%v", jobs, err)
	}
	if propertyCount != 3 || membershipCount != 4 || decisionCount != 3 {
		t.Fatalf("shadow state properties=%d memberships=%d decisions=%d", propertyCount, membershipCount, decisionCount)
	}

	if err := store.OverrideDuplicateDecision(ctx, acceptedDecisionID, domain.DuplicateRejected, "manual false-positive correction"); err != nil {
		t.Fatal(err)
	}
	splitProperty, err := store.ReassignListingProperty(ctx, secondID, nil, "manual split after rejected decision")
	if err != nil {
		t.Fatal(err)
	}
	if splitProperty == firstProperty {
		t.Fatalf("split retained merged property %d", splitProperty)
	}
	var historicalMemberships int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM property_listing_memberships WHERE listing_id=$1`, secondID).Scan(&historicalMemberships); err != nil {
		t.Fatal(err)
	}
	if historicalMemberships != 3 {
		t.Fatalf("listing membership history count = %d, want 3", historicalMemberships)
	}
	if err := store.pool.QueryRow(ctx, `SELECT automatic_status,status FROM duplicate_decisions WHERE id=$1`, acceptedDecisionID).Scan(&automaticStatus, &status); err != nil {
		t.Fatal(err)
	}
	if automaticStatus != string(domain.DuplicateAccepted) || status != string(domain.DuplicateRejected) {
		t.Fatalf("corrected decision = %s/%s", automaticStatus, status)
	}
	var notificationCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM notifications`).Scan(&notificationCount); err != nil || notificationCount != 0 {
		t.Fatalf("shadow resolver affected notifications: count=%d err=%v", notificationCount, err)
	}
}

func TestListingAvailabilityHistoryIsConservativeAndRetained(t *testing.T) {
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
	if _, err := store.pool.Exec(ctx, `TRUNCATE listing_availability_attempts,listing_availability_jobs,listing_availability_observations,duplicate_resolution_attempts,duplicate_resolution_jobs,property_listing_memberships,duplicate_decisions,properties,listing_photo_fingerprints,listing_signal_snapshots,listing_signal_attempts,listing_signal_jobs,consumed_events,outbox_events,notifications,filter_areas,listing_photos,listing_price_history,listings,source_state,filters,source_areas,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	listing := domain.Listing{Source: "ss.lv", ExternalID: "availability-one", URL: "https://www.ss.lv/msg/availability-one.html", DealType: domain.DealSale, PropertyType: domain.PropertyApartment, Title: "Retained listing", Description: "Original description", PriceEUR: intPointer(99000), Address: "Brīvības iela 48", AreaKey: "lv/riga/centrs", Rooms: intPointer(2), AreaM2: floatPointer(54), PhotoURLs: []string{"https://i.ss.lv/availability.jpg"}}
	listingID, propertyID := resolveIntegrationListing(t, store, listing, strings.Repeat("f", 64), strings.Repeat("a", 64), "0000000000000000")

	var status string
	var firstSeen, lastSeen, changedAt time.Time
	if err := store.pool.QueryRow(ctx, `SELECT availability_status,first_seen_at,last_seen_at,availability_changed_at FROM listings WHERE id=$1`, listingID).Scan(&status, &firstSeen, &lastSeen, &changedAt); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.AvailabilityActive) || !firstSeen.Equal(lastSeen) || !firstSeen.Equal(changedAt) {
		t.Fatalf("initial lifecycle = %s %v/%v/%v", status, firstSeen, lastSeen, changedAt)
	}
	var eventCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events`).Scan(&eventCount); err != nil || eventCount != 0 {
		t.Fatalf("baseline events=%d err=%v", eventCount, err)
	}

	if _, err := store.ProcessDiscovered(ctx, "integration:availability-empty", nil, true); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT availability_status FROM listings WHERE id=$1`, listingID).Scan(&status); err != nil || status != string(domain.AvailabilityActive) {
		t.Fatalf("feed absence changed status=%s err=%v", status, err)
	}

	jobs, err := store.ClaimAvailabilityJobs(ctx, 1, 24*time.Hour)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("fresh active listing was checked: jobs=%+v err=%v", jobs, err)
	}
	jobs, err = store.ClaimAvailabilityJobs(ctx, 1, time.Nanosecond)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("availability jobs=%+v err=%v", jobs, err)
	}
	if err := store.CompleteAvailabilityJob(ctx, jobs[0], domain.AvailabilityObservation{Status: domain.AvailabilityInactive, Evidence: "accessible archive without contacts"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT availability_status,last_seen_at,availability_changed_at FROM listings WHERE id=$1`, listingID).Scan(&status, &lastSeen, &changedAt); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.AvailabilityInactive) || !lastSeen.Equal(firstSeen) || !changedAt.After(firstSeen) {
		t.Fatalf("inactive lifecycle = %s last=%v changed=%v first=%v", status, lastSeen, changedAt, firstSeen)
	}
	activeOffers, err := store.CurrentPropertyOfferIDs(ctx, propertyID)
	if err != nil || len(activeOffers) != 0 {
		t.Fatalf("inactive current offers=%v err=%v", activeOffers, err)
	}
	historicalOffers, err := store.HistoricalPropertyOfferIDs(ctx, propertyID)
	if err != nil || len(historicalOffers) != 1 || historicalOffers[0] != listingID {
		t.Fatalf("historical offers=%v err=%v", historicalOffers, err)
	}
	var title string
	var priceHistory, snapshots, fingerprints int
	if err := store.pool.QueryRow(ctx, `SELECT title FROM listings WHERE id=$1`, listingID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM listing_price_history WHERE listing_id=$1`, listingID).Scan(&priceHistory); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM listing_signal_snapshots WHERE listing_id=$1`, listingID).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM listing_photo_fingerprints WHERE listing_id=$1`, listingID).Scan(&fingerprints); err != nil {
		t.Fatal(err)
	}
	if title != "Retained listing" || priceHistory != 1 || snapshots != 1 || fingerprints != 1 {
		t.Fatalf("retained title=%q price_history=%d snapshots=%d fingerprints=%d", title, priceHistory, snapshots, fingerprints)
	}

	if _, err := store.pool.Exec(ctx, `UPDATE listing_availability_jobs SET next_attempt_at=now(),priority_requested=TRUE WHERE listing_id=$1`, listingID); err != nil {
		t.Fatal(err)
	}
	jobs, err = store.ClaimAvailabilityJobs(ctx, 1, 24*time.Hour)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("inactive listing was rechecked: jobs=%+v err=%v", jobs, err)
	}
	if err := store.RecordFeedSightings(ctx, []domain.Listing{listing}); err != nil {
		t.Fatal(err)
	}
	jobs, err = store.ClaimAvailabilityJobs(ctx, 1, 24*time.Hour)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("reactivated priority claim=%+v err=%v", jobs, err)
	}
	if err := store.CompleteAvailabilityJob(ctx, jobs[0], domain.AvailabilityObservation{Status: domain.AvailabilityUnknown, Evidence: "provider challenge page"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM current_active_listing_offers WHERE id=$1`, listingID).Scan(&eventCount); err != nil || eventCount != 0 {
		t.Fatalf("unknown represented as active: count=%d err=%v", eventCount, err)
	}

	if _, err := store.pool.Exec(ctx, `UPDATE listing_availability_jobs SET next_attempt_at=now() WHERE listing_id=$1`, listingID); err != nil {
		t.Fatal(err)
	}
	jobs, err = store.ClaimAvailabilityJobs(ctx, 1, 24*time.Hour)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("retry availability claim=%+v err=%v", jobs, err)
	}
	if err := store.RetryAvailabilityJob(ctx, jobs[0], errors.New("temporary provider error")); err != nil {
		t.Fatal(err)
	}
	var attemptOutcome, lastError string
	if err := store.pool.QueryRow(ctx, `SELECT outcome,COALESCE(error,'') FROM listing_availability_attempts WHERE id=$1`, jobs[0].AttemptID).Scan(&attemptOutcome, &lastError); err != nil {
		t.Fatal(err)
	}
	if attemptOutcome != "failed" || !strings.Contains(lastError, "temporary provider error") {
		t.Fatalf("retry attempt=%s/%q", attemptOutcome, lastError)
	}

	beforeReappearance := time.Now().UTC()
	if _, err := store.ProcessDiscovered(ctx, "integration:availability", []domain.Listing{listing}, true); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT availability_status,last_seen_at FROM listings WHERE id=$1`, listingID).Scan(&status, &lastSeen); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.AvailabilityActive) || lastSeen.Before(beforeReappearance) {
		t.Fatalf("reactivated lifecycle=%s last=%v", status, lastSeen)
	}
	activeOffers, err = store.CurrentPropertyOfferIDs(ctx, propertyID)
	if err != nil || len(activeOffers) != 1 {
		t.Fatalf("reactivated current offers=%v err=%v", activeOffers, err)
	}
	var observationCount, transitionCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE is_transition) FROM listing_availability_observations WHERE listing_id=$1`, listingID).Scan(&observationCount, &transitionCount); err != nil {
		t.Fatal(err)
	}
	if observationCount != 5 || transitionCount != 4 {
		t.Fatalf("lifecycle observations=%d transitions=%d", observationCount, transitionCount)
	}
}

func TestDuplicateAwareNotificationsClassifyPropertyOffersAndRetainEventContext(t *testing.T) {
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
	if _, err := store.pool.Exec(ctx, `TRUNCATE listing_availability_attempts,listing_availability_jobs,listing_availability_observations,duplicate_resolution_attempts,duplicate_resolution_jobs,property_listing_memberships,duplicate_decisions,properties,listing_photo_fingerprints,listing_signal_snapshots,listing_signal_attempts,listing_signal_jobs,consumed_events,outbox_events,notifications,filter_areas,listing_photos,listing_price_history,listings,source_state,filters,source_areas,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, 88, 88, "Duplicate tester", "en")
	if err != nil {
		t.Fatal(err)
	}
	activated := time.Now().Add(-time.Minute)
	broadFilter, err := store.CreateFilter(ctx, domain.SearchFilter{UserID: user.ID, DealType: domain.DealSale, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, PriceMax: intPointer(200000), Enabled: true, ActivatedAt: &activated})
	if err != nil {
		t.Fatal(err)
	}
	valueFilter, err := store.CreateFilter(ctx, domain.SearchFilter{UserID: user.ID, DealType: domain.DealSale, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, PriceMax: intPointer(95000), Enabled: true, ActivatedAt: &activated})
	if err != nil {
		t.Fatal(err)
	}
	secondUser, err := store.UpsertUser(ctx, 89, 89, "Second duplicate tester", "en")
	if err != nil {
		t.Fatal(err)
	}
	secondUserFilter, err := store.CreateFilter(ctx, domain.SearchFilter{UserID: secondUser.ID, DealType: domain.DealSale, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, PriceMax: intPointer(200000), Enabled: true, ActivatedAt: &activated})
	if err != nil {
		t.Fatal(err)
	}

	base := domain.Listing{Source: "ss.lv", ExternalID: "property-a", URL: "https://www.ss.lv/msg/property-a.html", DealType: domain.DealSale, PropertyType: domain.PropertyApartment, Title: "Original home", PriceEUR: intPointer(100000), Address: "Brīvības iela 48", AreaKey: "lv/riga/centrs", Rooms: intPointer(2), AreaM2: floatPointer(54), Floor: intPointer(3), TotalFloors: intPointer(5), PhotoURLs: []string{"https://i.ss.lv/property-a.jpg"}}
	firstID, firstEvent := discoverIntegrationListing(t, store, base)
	if created, err := store.MatchDuplicateAwareListingEvent(ctx, firstEvent.ID, firstID, firstEvent.OccurredAt); err == nil || created != 0 {
		t.Fatalf("unresolved event created=%d err=%v", created, err)
	}
	var consumed int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM consumed_events WHERE event_id=$1::uuid`, firstEvent.ID).Scan(&consumed); err != nil || consumed != 0 {
		t.Fatalf("unresolved event was consumed: count=%d err=%v", consumed, err)
	}
	firstProperty := resolveDiscoveredListing(t, store, firstID, strings.Repeat("1", 64), strings.Repeat("a", 64), "0000000000000000")
	if created, err := store.MatchDuplicateAwareListingEvent(ctx, firstEvent.ID, firstID, firstEvent.OccurredAt); err != nil || created != 2 {
		t.Fatalf("first property match created=%d err=%v", created, err)
	}
	var firstUserFilterID int64
	if err := store.pool.QueryRow(ctx, `SELECT n.filter_id FROM notifications n JOIN filters f ON f.id=n.filter_id WHERE n.trigger_event_id=$1::uuid AND f.user_id=$2`, firstEvent.ID, user.ID).Scan(&firstUserFilterID); err != nil {
		t.Fatal(err)
	}
	if firstUserFilterID != broadFilter {
		t.Fatalf("first user's selected filter=%d want broad filter %d", firstUserFilterID, broadFilter)
	}
	propertyTx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	firstUserWasNotified, err := propertyWasNotified(ctx, propertyTx, firstProperty, user.ID)
	if rollbackErr := propertyTx.Rollback(ctx); err == nil && rollbackErr != nil {
		err = rollbackErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if !firstUserWasNotified {
		t.Fatal("first user's property history was not found through the matching broad filter")
	}
	if created, err := store.MatchDuplicateAwareListingEvent(ctx, firstEvent.ID, firstID, firstEvent.OccurredAt); err != nil || created != 0 {
		t.Fatalf("reprocessed first event created=%d err=%v", created, err)
	}
	setIntegrationAvailability(t, store, firstID, domain.AvailabilityInactive)

	lowerRelist := base
	lowerRelist.Source = "city24.lv"
	lowerRelist.ExternalID = "property-b"
	lowerRelist.URL = "https://www.city24.lv/real-estate/property-b"
	lowerRelist.Title = "Relisted lower"
	lowerRelist.PriceEUR = intPointer(90000)
	lowerRelist.PhotoURLs = []string{"https://static.img-city24.lv/property-b.jpg"}
	secondID, secondEvent := discoverIntegrationListing(t, store, lowerRelist)
	secondProperty := resolveDiscoveredListing(t, store, secondID, strings.Repeat("2", 64), strings.Repeat("a", 64), "0000000000000000")
	if secondProperty != firstProperty {
		t.Fatalf("lower relist property=%d, want %d", secondProperty, firstProperty)
	}
	if created, err := store.MatchDuplicateAwareListingEvent(ctx, secondEvent.ID, secondID, secondEvent.OccurredAt); err != nil || created != 2 {
		t.Fatalf("lower relist created=%d err=%v", created, err)
	}
	assertNotificationRows(t, store, secondID, domain.NotificationRelistingChanged, 2, 100000, 90000)
	setIntegrationAvailability(t, store, secondID, domain.AvailabilityInactive)

	higherRelist := base
	higherRelist.ExternalID = "property-c"
	higherRelist.URL = "https://www.ss.lv/msg/property-c.html"
	higherRelist.Title = "Relisted higher"
	higherRelist.PriceEUR = intPointer(110000)
	higherRelist.PhotoURLs = []string{"https://i.ss.lv/property-c.jpg"}
	thirdID, thirdEvent := discoverIntegrationListing(t, store, higherRelist)
	thirdProperty := resolveDiscoveredListing(t, store, thirdID, strings.Repeat("3", 64), strings.Repeat("a", 64), "0000000000000000")
	if thirdProperty != firstProperty {
		t.Fatalf("higher relist property=%d, want %d", thirdProperty, firstProperty)
	}
	if created, err := store.MatchDuplicateAwareListingEvent(ctx, thirdEvent.ID, thirdID, thirdEvent.OccurredAt); err != nil || created != 2 {
		t.Fatalf("higher relist created=%d err=%v", created, err)
	}
	assertNotificationRows(t, store, thirdID, domain.NotificationRelistingChanged, 2, 90000, 110000)

	cheaper := base
	cheaper.Source = "city24.lv"
	cheaper.ExternalID = "property-d"
	cheaper.URL = "https://www.city24.lv/real-estate/property-d"
	cheaper.Title = "Concurrent cheaper offer"
	cheaper.PriceEUR = intPointer(80000)
	cheaper.PhotoURLs = []string{"https://static.img-city24.lv/property-d.jpg"}
	fourthID, fourthEvent := discoverIntegrationListing(t, store, cheaper)
	fourthProperty := resolveDiscoveredListing(t, store, fourthID, strings.Repeat("4", 64), strings.Repeat("a", 64), "0000000000000000")
	if fourthProperty != firstProperty {
		t.Fatalf("cheaper property=%d, want %d", fourthProperty, firstProperty)
	}
	if created, err := store.MatchDuplicateAwareListingEvent(ctx, fourthEvent.ID, fourthID, fourthEvent.OccurredAt); err == nil || created != 0 {
		t.Fatalf("stale alternative availability created=%d err=%v", created, err)
	}
	setIntegrationAvailability(t, store, thirdID, domain.AvailabilityActive)
	if created, err := store.MatchDuplicateAwareListingEvent(ctx, fourthEvent.ID, fourthID, fourthEvent.OccurredAt); err != nil || created != 2 {
		t.Fatalf("cheaper offer created=%d err=%v", created, err)
	}
	assertNotificationRows(t, store, fourthID, domain.NotificationCheaperOffer, 2, 110000, 80000)
	var alternativesJSON []byte
	if err := store.pool.QueryRow(ctx, `SELECT active_alternatives FROM notifications WHERE listing_id=$1 AND notification_type='cheaper_offer' ORDER BY id LIMIT 1`, fourthID).Scan(&alternativesJSON); err != nil {
		t.Fatal(err)
	}
	var alternatives []domain.ListingAlternative
	if err := json.Unmarshal(alternativesJSON, &alternatives); err != nil {
		t.Fatal(err)
	}
	if len(alternatives) != 1 || alternatives[0].ListingID != thirdID || alternatives[0].PriceEUR == nil || *alternatives[0].PriceEUR != 110000 {
		t.Fatalf("active alternatives=%+v", alternatives)
	}

	higherConcurrent := base
	higherConcurrent.ExternalID = "property-e"
	higherConcurrent.URL = "https://www.ss.lv/msg/property-e.html"
	higherConcurrent.Title = "Concurrent higher offer"
	higherConcurrent.PriceEUR = intPointer(120000)
	higherConcurrent.PhotoURLs = []string{"https://i.ss.lv/property-e.jpg"}
	fifthID, fifthEvent := discoverIntegrationListing(t, store, higherConcurrent)
	resolveDiscoveredListing(t, store, fifthID, strings.Repeat("5", 64), strings.Repeat("a", 64), "0000000000000000")
	setIntegrationAvailability(t, store, thirdID, domain.AvailabilityActive)
	setIntegrationAvailability(t, store, fourthID, domain.AvailabilityUnknown)
	if created, err := store.MatchDuplicateAwareListingEvent(ctx, fifthEvent.ID, fifthID, fifthEvent.OccurredAt); err != nil || created != 0 {
		t.Fatalf("higher concurrent created=%d err=%v", created, err)
	}

	ambiguous := base
	ambiguous.ExternalID = "other-flat"
	ambiguous.URL = "https://www.ss.lv/msg/other-flat.html"
	ambiguous.Title = "Different flat in same building"
	ambiguous.PriceEUR = intPointer(85000)
	ambiguous.PhotoURLs = []string{"https://i.ss.lv/other-flat.jpg"}
	ambiguousID, ambiguousEvent := discoverIntegrationListing(t, store, ambiguous)
	ambiguousProperty := resolveDiscoveredListing(t, store, ambiguousID, strings.Repeat("6", 64), strings.Repeat("f", 64), "ffffffffffffffff")
	if ambiguousProperty == firstProperty {
		t.Fatalf("ambiguous listing merged into property %d", firstProperty)
	}
	if created, err := store.MatchDuplicateAwareListingEvent(ctx, ambiguousEvent.ID, ambiguousID, ambiguousEvent.OccurredAt); err != nil || created != 2 {
		t.Fatalf("ambiguous discovery created=%d err=%v", created, err)
	}
	assertNotificationRows(t, store, ambiguousID, domain.NotificationListingDiscovered, 2, -1, -1)

	legacy := base
	legacy.Source = "city24.lv"
	legacy.ExternalID = "property-rollout-off"
	legacy.URL = "https://www.city24.lv/real-estate/property-rollout-off"
	legacy.Title = "Rollout disabled path"
	legacy.PriceEUR = intPointer(130000)
	legacy.PhotoURLs = []string{"https://static.img-city24.lv/property-rollout-off.jpg"}
	legacyID, legacyEvent := discoverIntegrationListing(t, store, legacy)
	resolveDiscoveredListing(t, store, legacyID, strings.Repeat("7", 64), strings.Repeat("a", 64), "0000000000000000")
	if created, err := store.MatchListingEvent(ctx, legacyEvent.ID, legacyID, legacyEvent.OccurredAt); err != nil || created != 2 {
		t.Fatalf("rollout-disabled legacy path created=%d err=%v", created, err)
	}

	cheaper.PriceEUR = intPointer(85000)
	if result, err := store.ProcessDiscovered(ctx, "integration:duplicate-aware", []domain.Listing{cheaper}, true); err != nil || result.Events != 1 {
		t.Fatalf("existing offer price update=%+v err=%v", result, err)
	}
	priceEvent := latestPriceEvent(t, store)
	priceChange, err := events.DecodeListingPriceChanged(priceEvent)
	if err != nil {
		t.Fatal(err)
	}
	if created, err := store.MatchPriceChangedEvent(ctx, priceEvent.ID, priceChange, priceEvent.OccurredAt); err != nil || created != 2 {
		t.Fatalf("existing price change created=%d err=%v", created, err)
	}
	assertNotificationRows(t, store, fourthID, domain.NotificationPriceChanged, 2, 80000, 85000)

	pending, err := store.Pending(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	var cheaperSnapshot, priceSnapshot bool
	for _, item := range pending {
		if item.ListingID != fourthID {
			continue
		}
		switch item.Type {
		case domain.NotificationCheaperOffer:
			cheaperSnapshot = item.Listing.PriceEUR != nil && *item.Listing.PriceEUR == 80000 && len(item.Alternatives) == 1
		case domain.NotificationPriceChanged:
			priceSnapshot = item.Listing.PriceEUR != nil && *item.Listing.PriceEUR == 85000 && item.PreviousPriceEUR != nil && *item.PreviousPriceEUR == 80000
		}
	}
	if !cheaperSnapshot || !priceSnapshot {
		t.Fatalf("event snapshots cheaper=%t price=%t pending=%+v", cheaperSnapshot, priceSnapshot, pending)
	}
	if broadFilter == valueFilter {
		t.Fatal("test filters unexpectedly share an ID")
	}
	if secondUserFilter == broadFilter || secondUserFilter == valueFilter {
		t.Fatal("second user's filter unexpectedly shares an ID")
	}
	for _, eventID := range []string{firstEvent.ID, secondEvent.ID, thirdEvent.ID, fourthEvent.ID, ambiguousEvent.ID, legacyEvent.ID, priceEvent.ID} {
		assertNotificationUsers(t, store, eventID, user.ID, secondUser.ID)
	}
}

func TestDuplicateAwareMatchSchedulesAvailabilityForLaterPropertyMember(t *testing.T) {
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
	store, err := Open(ctx, databaseURL, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.pool.Exec(ctx, `TRUNCATE listing_availability_attempts,listing_availability_jobs,listing_availability_observations,duplicate_resolution_attempts,duplicate_resolution_jobs,property_listing_memberships,duplicate_decisions,properties,listing_photo_fingerprints,listing_signal_snapshots,listing_signal_attempts,listing_signal_jobs,consumed_events,outbox_events,notifications,filter_areas,listing_photos,listing_price_history,listings,source_state,filters,source_areas,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, 188, 188, "Late member tester", "en")
	if err != nil {
		t.Fatal(err)
	}
	activated := time.Now().Add(-time.Minute)
	if _, err := store.CreateFilter(ctx, domain.SearchFilter{UserID: user.ID, DealType: domain.DealSale, PropertyTypes: []domain.PropertyType{domain.PropertyApartment}, PriceMax: intPointer(200000), Enabled: true, ActivatedAt: &activated}); err != nil {
		t.Fatal(err)
	}

	first := domain.Listing{Source: "ss.lv", ExternalID: "late-member-a", URL: "https://www.ss.lv/msg/late-member-a.html", DealType: domain.DealSale, PropertyType: domain.PropertyApartment, Title: "First advert", PriceEUR: intPointer(100000), Address: "Brīvības iela 48", AreaKey: "lv/riga/centrs", Rooms: intPointer(2), AreaM2: floatPointer(54), Floor: intPointer(3), TotalFloors: intPointer(5), PhotoURLs: []string{"https://i.ss.lv/late-member-a.jpg"}}
	firstID, firstEvent := discoverIntegrationListing(t, store, first)
	firstProperty := resolveDiscoveredListing(t, store, firstID, strings.Repeat("8", 64), strings.Repeat("b", 64), "1111111111111111")

	second := first
	second.Source = "city24.lv"
	second.ExternalID = "late-member-b"
	second.URL = "https://www.city24.lv/real-estate/late-member-b"
	second.Title = "Later duplicate"
	second.PhotoURLs = []string{"https://static.img-city24.lv/late-member-b.jpg"}
	secondID, _ := discoverIntegrationListing(t, store, second)
	secondProperty := resolveDiscoveredListing(t, store, secondID, strings.Repeat("9", 64), strings.Repeat("b", 64), "1111111111111111")
	if secondProperty != firstProperty {
		t.Fatalf("later listing property=%d, want %d", secondProperty, firstProperty)
	}

	var priority bool
	if err := store.pool.QueryRow(ctx, `SELECT priority_requested FROM listing_availability_jobs WHERE listing_id=$1`, secondID).Scan(&priority); err != nil {
		t.Fatal(err)
	}
	if priority {
		t.Fatal("later listing unexpectedly started with a priority availability check")
	}

	created, err := store.MatchDuplicateAwareListingEvent(ctx, firstEvent.ID, firstID, firstEvent.OccurredAt)
	if created != 0 || !events.IsDependencyPending(err) {
		t.Fatalf("pending match created=%d err=%v", created, err)
	}
	var due bool
	if err := store.pool.QueryRow(ctx, `SELECT priority_requested AND next_attempt_at<=now() FROM listing_availability_jobs WHERE listing_id=$1`, secondID).Scan(&due); err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("missing availability evidence did not schedule the later listing immediately")
	}

	jobs, err := store.ClaimAvailabilityJobs(ctx, 10, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	completedSecond := false
	for _, job := range jobs {
		if err := store.CompleteAvailabilityJob(ctx, job, domain.AvailabilityObservation{Status: domain.AvailabilityActive, Evidence: "integration priority check"}, 24*time.Hour); err != nil {
			t.Fatal(err)
		}
		completedSecond = completedSecond || job.Listing.ID == secondID
	}
	if !completedSecond {
		t.Fatalf("priority availability jobs did not include later listing %d: %+v", secondID, jobs)
	}

	if created, err := store.MatchDuplicateAwareListingEvent(ctx, firstEvent.ID, firstID, firstEvent.OccurredAt); err != nil || created != 1 {
		t.Fatalf("ready match created=%d err=%v", created, err)
	}
	var consumed int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM consumed_events WHERE event_id=$1::uuid`, firstEvent.ID).Scan(&consumed); err != nil || consumed != 1 {
		t.Fatalf("ready event consumed=%d err=%v", consumed, err)
	}
}

func TestConcurrentDuplicateResolutionCommitsSerialize(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := Open(ctx, databaseURL, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.pool.Exec(ctx, `TRUNCATE listing_availability_attempts,listing_availability_jobs,listing_availability_observations,duplicate_resolution_attempts,duplicate_resolution_jobs,property_listing_memberships,duplicate_decisions,properties,listing_photo_fingerprints,listing_signal_snapshots,listing_signal_attempts,listing_signal_jobs,consumed_events,outbox_events,notifications,filter_areas,listing_photos,listing_price_history,listings,source_state,filters,source_areas,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	base := domain.Listing{Source: "ss.lv", ExternalID: "concurrent-a", URL: "https://www.ss.lv/msg/concurrent-a.html", DealType: domain.DealSale, PropertyType: domain.PropertyApartment, Title: "Concurrent A", PriceEUR: intPointer(100000), Address: "Dzirnavu iela 10", AreaKey: "lv/riga/centrs", Rooms: intPointer(2), AreaM2: floatPointer(50), Floor: intPointer(2), TotalFloors: intPointer(5), PhotoURLs: []string{"https://i.ss.lv/concurrent-a.jpg"}}
	firstID, _ := discoverIntegrationListing(t, store, base)
	second := base
	second.Source = "city24.lv"
	second.ExternalID = "concurrent-b"
	second.URL = "https://www.city24.lv/real-estate/concurrent-b"
	second.Title = "Concurrent B"
	second.PhotoURLs = []string{"https://static.img-city24.lv/concurrent-b.jpg"}
	secondID, _ := discoverIntegrationListing(t, store, second)

	signalJobs, err := store.ClaimListingSignalJobs(ctx, 2)
	if err != nil || len(signalJobs) != 2 {
		t.Fatalf("signal jobs=%+v err=%v", signalJobs, err)
	}
	for index, job := range signalJobs {
		signals := testListingSignals(job.Listing, strings.Repeat(string(rune('a'+index)), 64))
		signals.Listing.AreaKey = "lv/riga/centrs"
		signals.Photos[0].ExactHash = strings.Repeat("c", 64)
		signals.Photos[0].PerceptualHash = "2222222222222222"
		if err := store.CompleteListingSignalJob(ctx, job, signals); err != nil {
			t.Fatal(err)
		}
	}
	resolutionJobs, err := store.ClaimDuplicateResolutionJobs(ctx, 2)
	if err != nil || len(resolutionJobs) != 2 {
		t.Fatalf("resolution jobs=%+v err=%v", resolutionJobs, err)
	}
	jobsByListing := make(map[int64]domain.DuplicateResolutionJob, len(resolutionJobs))
	for _, job := range resolutionJobs {
		jobsByListing[job.Evidence.Listing.ID] = job
	}
	firstJob, firstOK := jobsByListing[firstID]
	secondJob, secondOK := jobsByListing[secondID]
	if !firstOK || !secondOK {
		t.Fatalf("resolution jobs missing listings %d/%d: %+v", firstID, secondID, resolutionJobs)
	}
	firstDecision := identity.EvaluateDuplicate(firstJob.Evidence, secondJob.Evidence)
	secondDecision := identity.EvaluateDuplicate(secondJob.Evidence, firstJob.Evidence)
	if firstDecision.Status != domain.DuplicateAccepted || secondDecision.Status != domain.DuplicateAccepted {
		t.Fatalf("concurrent decisions not accepted: %+v / %+v", firstDecision, secondDecision)
	}

	blocker, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if err := lockDuplicateResolutionCommit(ctx, blocker); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 2)
	results := make(chan error, 2)
	for _, input := range []struct {
		job      domain.DuplicateResolutionJob
		decision domain.DuplicateDecision
	}{{firstJob, firstDecision}, {secondJob, secondDecision}} {
		input := input
		go func() {
			started <- struct{}{}
			results <- store.CompleteDuplicateResolutionJob(ctx, input.job, []domain.DuplicateDecision{input.decision})
		}()
	}
	<-started
	<-started
	select {
	case err := <-results:
		t.Fatalf("resolver commit bypassed serialization lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

	var completed, properties int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM duplicate_resolution_jobs WHERE status='completed'`).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(DISTINCT property_id) FROM property_listing_memberships WHERE listing_id IN ($1,$2) AND valid_to IS NULL`, firstID, secondID).Scan(&properties); err != nil {
		t.Fatal(err)
	}
	if completed != 2 || properties != 1 {
		t.Fatalf("serialized resolution completed=%d properties=%d", completed, properties)
	}
}

func discoverIntegrationListing(t *testing.T, store *Store, listing domain.Listing) (int64, events.Envelope) {
	t.Helper()
	ctx := context.Background()
	result, err := store.ProcessDiscovered(ctx, "integration:duplicate-aware", []domain.Listing{listing}, true)
	if err != nil || result.Inserted != 1 || result.Events != 1 {
		t.Fatalf("discovery %s/%s result=%+v err=%v", listing.Source, listing.ExternalID, result, err)
	}
	var listingID int64
	if err := store.pool.QueryRow(ctx, `SELECT id FROM listings WHERE source=$1 AND external_id=$2`, listing.Source, listing.ExternalID).Scan(&listingID); err != nil {
		t.Fatal(err)
	}
	var event events.Envelope
	if err := store.pool.QueryRow(ctx, `SELECT id::text,event_type,occurred_at,payload FROM outbox_events WHERE aggregate_id=$1 AND event_type=$2 ORDER BY occurred_at DESC,id DESC LIMIT 1`, listingID, events.ListingDiscoveredV1).Scan(&event.ID, &event.Type, &event.OccurredAt, &event.Data); err != nil {
		t.Fatal(err)
	}
	return listingID, event
}

func resolveDiscoveredListing(t *testing.T, store *Store, listingID int64, inputHash, exactHash, perceptualHash string) int64 {
	t.Helper()
	ctx := context.Background()
	signalJobs, err := store.ClaimListingSignalJobs(ctx, 1)
	if err != nil || len(signalJobs) != 1 || signalJobs[0].Listing.ID != listingID {
		t.Fatalf("signal jobs for %d=%+v err=%v", listingID, signalJobs, err)
	}
	signals := testListingSignals(signalJobs[0].Listing, inputHash)
	signals.Listing.AreaKey = "lv/riga/centrs"
	signals.Photos[0].ExactHash = exactHash
	signals.Photos[0].PerceptualHash = perceptualHash
	if err := store.CompleteListingSignalJob(ctx, signalJobs[0], signals); err != nil {
		t.Fatal(err)
	}
	resolutionJobs, err := store.ClaimDuplicateResolutionJobs(ctx, 1)
	if err != nil || len(resolutionJobs) != 1 || resolutionJobs[0].Evidence.Listing.ID != listingID {
		t.Fatalf("resolution jobs for %d=%+v err=%v", listingID, resolutionJobs, err)
	}
	candidates, err := store.DuplicateCandidates(ctx, resolutionJobs[0].Evidence, 50)
	if err != nil {
		t.Fatal(err)
	}
	decisions := make([]domain.DuplicateDecision, 0, len(candidates))
	for _, candidate := range candidates {
		decisions = append(decisions, identity.EvaluateDuplicate(resolutionJobs[0].Evidence, candidate))
	}
	if err := store.CompleteDuplicateResolutionJob(ctx, resolutionJobs[0], decisions); err != nil {
		t.Fatal(err)
	}
	var propertyID int64
	if err := store.pool.QueryRow(ctx, `SELECT property_id FROM property_listing_memberships WHERE listing_id=$1 AND valid_to IS NULL`, listingID).Scan(&propertyID); err != nil {
		t.Fatal(err)
	}
	return propertyID
}

func setIntegrationAvailability(t *testing.T, store *Store, listingID int64, status domain.AvailabilityStatus) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `UPDATE listing_availability_jobs SET next_attempt_at=now()+interval '7 days' WHERE status='pending' AND listing_id<>$1`, listingID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE listing_availability_jobs SET next_attempt_at=now(),priority_requested=TRUE WHERE listing_id=$1`, listingID); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.ClaimAvailabilityJobs(ctx, 1, 24*time.Hour)
	if err != nil || len(jobs) != 1 || jobs[0].Listing.ID != listingID {
		t.Fatalf("availability jobs for %d=%+v err=%v", listingID, jobs, err)
	}
	if err := store.CompleteAvailabilityJob(ctx, jobs[0], domain.AvailabilityObservation{Status: status, Evidence: "integration lifecycle transition"}, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
}

func assertNotificationRows(t *testing.T, store *Store, listingID int64, notificationType domain.NotificationType, wantCount, wantPrevious, wantCurrent int) {
	t.Helper()
	ctx := context.Background()
	rows, err := store.pool.Query(ctx, `SELECT previous_price_eur,current_price_eur FROM notifications WHERE listing_id=$1 AND notification_type=$2`, listingID, notificationType)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var previous, current *int
		if err := rows.Scan(&previous, &current); err != nil {
			t.Fatal(err)
		}
		if wantPrevious >= 0 && (previous == nil || *previous != wantPrevious) {
			t.Fatalf("previous price=%v want=%d", previous, wantPrevious)
		}
		if wantCurrent >= 0 && (current == nil || *current != wantCurrent) {
			t.Fatalf("current price=%v want=%d", current, wantCurrent)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != wantCount {
		t.Fatalf("notifications for listing=%d type=%s count=%d want=%d", listingID, notificationType, count, wantCount)
	}
}

func assertNotificationUsers(t *testing.T, store *Store, eventID string, wantUserIDs ...int64) {
	t.Helper()
	ctx := context.Background()
	rows, err := store.pool.Query(ctx, `SELECT f.user_id,count(*) FROM notifications n JOIN filters f ON f.id=n.filter_id WHERE n.trigger_event_id=$1::uuid GROUP BY f.user_id`, eventID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	actual := make(map[int64]int)
	for rows.Next() {
		var userID int64
		var count int
		if err := rows.Scan(&userID, &count); err != nil {
			t.Fatal(err)
		}
		actual[userID] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(wantUserIDs) {
		t.Fatalf("notification users for event %s = %v, want %v", eventID, actual, wantUserIDs)
	}
	for _, userID := range wantUserIDs {
		if actual[userID] != 1 {
			t.Fatalf("notifications for event %s user %d = %d, want 1", eventID, userID, actual[userID])
		}
	}
}

func resolveIntegrationListing(t *testing.T, store *Store, listing domain.Listing, inputHash, exactHash, perceptualHash string) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	result, err := store.ProcessDiscovered(ctx, "integration:duplicates", []domain.Listing{listing}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Inserted != 1 {
		t.Fatalf("listing %s/%s discovery = %+v", listing.Source, listing.ExternalID, result)
	}
	signalJobs, err := store.ClaimListingSignalJobs(ctx, 1)
	if err != nil || len(signalJobs) != 1 {
		t.Fatalf("signal jobs=%+v err=%v", signalJobs, err)
	}
	signals := testListingSignals(signalJobs[0].Listing, inputHash)
	signals.Listing.AreaKey = listing.AreaKey
	signals.Listing.Rooms = listing.Rooms
	signals.Listing.AreaM2 = listing.AreaM2
	signals.Listing.Floor = listing.Floor
	signals.Listing.TotalFloors = listing.TotalFloors
	signals.Photos[0].SourceURL = listing.PhotoURLs[0]
	signals.Photos[0].ExactHash = exactHash
	signals.Photos[0].PerceptualHash = perceptualHash
	if err := store.CompleteListingSignalJob(ctx, signalJobs[0], signals); err != nil {
		t.Fatal(err)
	}
	resolutionJobs, err := store.ClaimDuplicateResolutionJobs(ctx, 1)
	if err != nil || len(resolutionJobs) != 1 {
		t.Fatalf("resolution jobs=%+v err=%v", resolutionJobs, err)
	}
	candidates, err := store.DuplicateCandidates(ctx, resolutionJobs[0].Evidence, 50)
	if err != nil {
		t.Fatal(err)
	}
	decisions := make([]domain.DuplicateDecision, 0, len(candidates))
	for _, candidate := range candidates {
		decisions = append(decisions, identity.EvaluateDuplicate(resolutionJobs[0].Evidence, candidate))
	}
	if err := store.CompleteDuplicateResolutionJob(ctx, resolutionJobs[0], decisions); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteDuplicateResolutionJob(ctx, resolutionJobs[0], decisions); err != nil {
		t.Fatal(err)
	}
	var propertyID int64
	if err := store.pool.QueryRow(ctx, `SELECT property_id FROM property_listing_memberships WHERE listing_id=$1 AND valid_to IS NULL`, signalJobs[0].Listing.ID).Scan(&propertyID); err != nil {
		t.Fatal(err)
	}
	return signalJobs[0].Listing.ID, propertyID
}

func testListingSignals(listing domain.Listing, inputHash string) domain.ListingSignals {
	listing.Description = "Complete useful description"
	listing.NormalizedAddress = "brivibas iela 48"
	listing.NormalizedDescription = "complete useful description"
	return domain.ListingSignals{
		Listing:              listing,
		InputHash:            inputHash,
		NormalizationVersion: "text-nfkd-v1",
		Photos: []domain.PhotoFingerprint{{
			Position:            0,
			SourceURL:           listing.PhotoURLs[0],
			ExactHash:           strings.Repeat("c", 64),
			ExactAlgorithm:      "sha256-v1",
			PerceptualHash:      strings.Repeat("d", 16),
			PerceptualAlgorithm: "dhash-64-v1",
			MediaType:           "image/jpeg",
			ByteSize:            1024,
			Width:               800,
			Height:              600,
		}},
	}
}

func assertSignalState(t *testing.T, store *Store, listingID int64, snapshots, attempts int, status string) {
	t.Helper()
	ctx := context.Background()
	var actualSnapshots, actualFingerprints, actualAttempts int
	var actualStatus string
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM listing_signal_snapshots WHERE listing_id=$1`, listingID).Scan(&actualSnapshots); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM listing_photo_fingerprints WHERE listing_id=$1`, listingID).Scan(&actualFingerprints); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM listing_signal_attempts WHERE listing_id=$1`, listingID).Scan(&actualAttempts); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT status FROM listing_signal_jobs WHERE listing_id=$1`, listingID).Scan(&actualStatus); err != nil {
		t.Fatal(err)
	}
	if actualSnapshots != snapshots || actualFingerprints != snapshots || actualAttempts != attempts || actualStatus != status {
		t.Fatalf("signal state snapshots=%d fingerprints=%d attempts=%d status=%s", actualSnapshots, actualFingerprints, actualAttempts, actualStatus)
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

func floatPointer(v float64) *float64 {
	return &v
}
