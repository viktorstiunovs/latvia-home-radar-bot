package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
)

type signalStoreStub struct {
	completed   *domain.ListingSignals
	reusable    []domain.PhotoFingerprint
	reusableErr error
	retried     error
}

func (s *signalStoreStub) ReusablePhotoFingerprints(context.Context, int64) ([]domain.PhotoFingerprint, error) {
	return s.reusable, s.reusableErr
}

func (s *signalStoreStub) ClaimListingSignalJobs(context.Context, int) ([]domain.ListingSignalJob, error) {
	return nil, nil
}

func (s *signalStoreStub) CompleteListingSignalJob(_ context.Context, _ domain.ListingSignalJob, signals domain.ListingSignals) error {
	s.completed = &signals
	return nil
}

func (s *signalStoreStub) RetryListingSignalJob(_ context.Context, _ domain.ListingSignalJob, cause error) error {
	s.retried = cause
	return nil
}

type signalSourceStub struct {
	listing domain.Listing
	err     error
}

func (s *signalSourceStub) Key() string {
	return "ss.lv:latvia:apartments:rent"
}

func (s *signalSourceStub) FetchRecent(context.Context) ([]domain.Listing, error) {
	return nil, nil
}

func (s *signalSourceStub) Enrich(context.Context, domain.Listing) (domain.Listing, error) {
	return s.listing, s.err
}

func TestListingSignalCollectorCompletesNormalizedSnapshot(t *testing.T) {
	store := &signalStoreStub{}
	listing := domain.Listing{ID: 42, Source: "ss.lv", ExternalID: "abc", PropertyType: domain.PropertyApartment, DealType: domain.DealRent, Title: "Concise", Description: "<b>Gaišs dzīvoklis!</b>", Address: "Brīvības iela 48"}
	source := &signalSourceStub{listing: listing}
	collector := NewListingSignalCollector(store, []provider.Source{source}, http.DefaultClient, 2, 8, slog.New(slog.NewTextHandler(io.Discard, nil)))

	collector.process(context.Background(), domain.ListingSignalJob{Listing: listing, Generation: 1, AttemptID: 2})

	if store.retried != nil || store.completed == nil {
		t.Fatalf("completed=%+v retried=%v", store.completed, store.retried)
	}
	if store.completed.Listing.Description != "<b>Gaišs dzīvoklis!</b>" || store.completed.Listing.NormalizedDescription != "gaiss dzivoklis" || store.completed.Listing.NormalizedAddress != "brivibas iela 48" {
		t.Fatalf("unexpected normalized signals: %+v", store.completed.Listing)
	}
	if len(store.completed.InputHash) != 64 || store.completed.NormalizationVersion == "" {
		t.Fatalf("unexpected snapshot identity: %+v", store.completed)
	}
}

func TestListingSignalCollectorSchedulesRetry(t *testing.T) {
	store := &signalStoreStub{}
	listing := domain.Listing{ID: 42, Source: "ss.lv", ExternalID: "abc", PropertyType: domain.PropertyApartment, DealType: domain.DealRent}
	source := &signalSourceStub{err: errors.New("provider unavailable")}
	collector := NewListingSignalCollector(store, []provider.Source{source}, http.DefaultClient, 1, 1, slog.New(slog.NewTextHandler(io.Discard, nil)))

	collector.process(context.Background(), domain.ListingSignalJob{Listing: listing, Generation: 1, AttemptID: 2})

	if store.completed != nil || store.retried == nil {
		t.Fatalf("completed=%+v retried=%v", store.completed, store.retried)
	}
}
