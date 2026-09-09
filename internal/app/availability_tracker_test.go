package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
)

type availabilityStoreStub struct {
	completed   *domain.AvailabilityObservation
	interval    time.Duration
	retried     error
	completeErr error
}

func (s *availabilityStoreStub) ClaimAvailabilityJobs(context.Context, int, time.Duration) ([]domain.AvailabilityJob, error) {
	return nil, nil
}

func (s *availabilityStoreStub) CompleteAvailabilityJob(_ context.Context, _ domain.AvailabilityJob, observation domain.AvailabilityObservation, interval time.Duration) error {
	s.completed = &observation
	s.interval = interval
	return s.completeErr
}

func (s *availabilityStoreStub) RetryAvailabilityJob(_ context.Context, _ domain.AvailabilityJob, cause error) error {
	s.retried = cause
	return nil
}

type availabilitySourceStub struct {
	observation domain.AvailabilityObservation
	err         error
}

func (s *availabilitySourceStub) Key() string {
	return "ss.lv:latvia:apartments:rent"
}

func (s *availabilitySourceStub) FetchRecent(context.Context) ([]domain.Listing, error) {
	return nil, nil
}

func (s *availabilitySourceStub) Enrich(_ context.Context, listing domain.Listing) (domain.Listing, error) {
	return listing, nil
}

func (s *availabilitySourceStub) CheckAvailability(context.Context, domain.Listing) (domain.AvailabilityObservation, error) {
	return s.observation, s.err
}

func TestAvailabilityTrackerCompletesProviderObservation(t *testing.T) {
	store := &availabilityStoreStub{}
	source := &availabilitySourceStub{observation: domain.AvailabilityObservation{Status: domain.AvailabilityInactive, Evidence: "archived"}}
	tracker := NewAvailabilityTracker(store, []provider.Source{source}, 3, 12*time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))

	tracker.process(context.Background(), domain.AvailabilityJob{Listing: domain.Listing{ID: 42, Source: "ss.lv"}, AttemptID: 7})

	if store.retried != nil || store.completed == nil || store.completed.Status != domain.AvailabilityInactive || store.interval != 12*time.Hour {
		t.Fatalf("completed=%+v interval=%v retried=%v", store.completed, store.interval, store.retried)
	}
}

func TestAvailabilityTrackerRetriesProviderAndStoreFailures(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	job := domain.AvailabilityJob{Listing: domain.Listing{ID: 42, Source: "ss.lv"}, AttemptID: 7}

	providerStore := &availabilityStoreStub{}
	providerFailure := &availabilitySourceStub{err: errors.New("provider unavailable")}
	NewAvailabilityTracker(providerStore, []provider.Source{providerFailure}, 1, time.Hour, logger).process(context.Background(), job)
	if providerStore.retried == nil || providerStore.completed != nil {
		t.Fatalf("provider failure completed=%+v retried=%v", providerStore.completed, providerStore.retried)
	}

	completionStore := &availabilityStoreStub{completeErr: errors.New("commit failed")}
	source := &availabilitySourceStub{observation: domain.AvailabilityObservation{Status: domain.AvailabilityActive, Evidence: "published"}}
	NewAvailabilityTracker(completionStore, []provider.Source{source}, 1, time.Hour, logger).process(context.Background(), job)
	if completionStore.retried == nil {
		t.Fatal("completion failure was not retried")
	}
}

func TestAvailabilityTrackerRetriesUnsupportedProvider(t *testing.T) {
	store := &availabilityStoreStub{}
	tracker := NewAvailabilityTracker(store, nil, 1, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))

	tracker.process(context.Background(), domain.AvailabilityJob{Listing: domain.Listing{ID: 42, Source: "unknown"}})

	if store.retried == nil {
		t.Fatal("unsupported provider was not retried")
	}
}
