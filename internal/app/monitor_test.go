package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
)

type fakeStore struct {
	initialized bool
	processed   []domain.Listing
	sightings   []domain.Listing
	notify      bool
}

func (f *fakeStore) RecordFeedSightings(_ context.Context, listings []domain.Listing) error {
	f.sightings = listings
	return nil
}

func (f *fakeStore) IsSourceInitialized(context.Context, string) (bool, error) {
	return f.initialized, nil
}

func (f *fakeStore) EnrichmentKeys(_ context.Context, l []domain.Listing) (map[domain.ListingKey]struct{}, error) {
	r := map[domain.ListingKey]struct{}{}
	for _, x := range l {
		r[x.Key()] = struct{}{}
	}
	return r, nil
}

func (f *fakeStore) ProcessDiscovered(_ context.Context, _ string, l []domain.Listing, n bool) (domain.DiscoveryResult, error) {
	f.processed = l
	f.notify = n
	return domain.DiscoveryResult{Inserted: len(l)}, nil
}

type fakeSource struct {
	listing domain.Listing
	failed  bool
	calls   int
}

func (f *fakeSource) Key() string {
	return "fake:apartments:rent"
}

func (f *fakeSource) FetchRecent(context.Context) ([]domain.Listing, error) {
	return []domain.Listing{f.listing}, nil
}

func (f *fakeSource) Enrich(_ context.Context, l domain.Listing) (domain.Listing, error) {
	f.calls++
	if !f.failed {
		l.DetailsEnriched = true
		l.Floor = ptrInt(2)
	}
	return l, nil
}

func ptrInt(v int) *int {
	return &v
}

func TestMonitorBaselineAndEnrichment(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	listing := domain.Listing{Source: "fake", ExternalID: "one", PropertyType: domain.PropertyApartment, DealType: domain.DealRent}
	store := &fakeStore{}
	source := &fakeSource{listing: listing}
	m := NewMonitor(store, []provider.Source{}, 0, logger)
	if err := m.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if source.calls != 0 || store.notify || len(store.sightings) != 1 {
		t.Fatal("baseline should neither enrich nor notify")
	}
	store.initialized = true
	if err := m.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 || len(store.processed) != 1 || !store.processed[0].DetailsEnriched {
		t.Fatal("new listing was not enriched before processing")
	}
	source.failed = true
	if err := m.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if len(store.processed) != 0 {
		t.Fatal("failed enrichment should not be persisted")
	}
}
