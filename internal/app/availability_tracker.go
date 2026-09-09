package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
)

type AvailabilityTracker struct {
	store    AvailabilityStore
	checkers map[string]provider.AvailabilityChecker
	workers  int
	interval time.Duration
	logger   *slog.Logger
}

func NewAvailabilityTracker(store AvailabilityStore, sources []provider.Source, workers int, interval time.Duration, logger *slog.Logger) *AvailabilityTracker {
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	checkers := make(map[string]provider.AvailabilityChecker)
	for _, source := range sources {
		checker, ok := source.(provider.AvailabilityChecker)
		if !ok {
			continue
		}
		if _, exists := checkers[providerName(source.Key())]; !exists {
			checkers[providerName(source.Key())] = checker
		}
	}
	return &AvailabilityTracker{store: store, checkers: checkers, workers: workers, interval: interval, logger: logger}
}

func (t *AvailabilityTracker) Run(ctx context.Context) error {
	for {
		jobs, err := t.store.ClaimAvailabilityJobs(ctx, t.workers, t.interval)
		if err != nil {
			return fmt.Errorf("claim availability jobs: %w", err)
		}
		if len(jobs) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}
		var wait sync.WaitGroup
		for _, job := range jobs {
			job := job
			wait.Add(1)
			go func() {
				defer wait.Done()
				t.process(ctx, job)
			}()
		}
		wait.Wait()
	}
}

func (t *AvailabilityTracker) process(ctx context.Context, job domain.AvailabilityJob) {
	checker := t.checkers[job.Listing.Source]
	if checker == nil {
		t.retry(ctx, job, fmt.Errorf("no availability checker for provider %q", job.Listing.Source))
		return
	}
	observation, err := checker.CheckAvailability(ctx, job.Listing)
	if err != nil {
		t.retry(ctx, job, fmt.Errorf("check provider availability: %w", err))
		return
	}
	if err := t.store.CompleteAvailabilityJob(ctx, job, observation, t.interval); err != nil {
		t.retry(ctx, job, fmt.Errorf("complete availability check: %w", err))
		return
	}
	t.logger.Info("listing availability checked", "listing_id", job.Listing.ID, "source", job.Listing.Source, "status", observation.Status, "evidence", observation.Evidence)
}

func (t *AvailabilityTracker) retry(ctx context.Context, job domain.AvailabilityJob, cause error) {
	t.logger.Warn("listing availability check failed", "listing_id", job.Listing.ID, "source", job.Listing.Source, "error", cause)
	if err := t.store.RetryAvailabilityJob(ctx, job, cause); err != nil {
		t.logger.Error("availability retry scheduling failed", "listing_id", job.Listing.ID, "error", err)
	}
}

func providerName(key string) string {
	for index, char := range key {
		if char == ':' {
			return key[:index]
		}
	}
	return key
}
