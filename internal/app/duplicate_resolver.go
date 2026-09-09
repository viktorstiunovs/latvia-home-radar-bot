package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/identity"
)

type DuplicateResolver struct {
	store          DuplicateResolverStore
	workers        int
	candidateLimit int
	logger         *slog.Logger
}

func NewDuplicateResolver(store DuplicateResolverStore, workers, candidateLimit int, logger *slog.Logger) *DuplicateResolver {
	if workers < 1 {
		workers = 1
	}
	if candidateLimit < 1 {
		candidateLimit = 50
	}
	if candidateLimit > 100 {
		candidateLimit = 100
	}
	return &DuplicateResolver{store: store, workers: workers, candidateLimit: candidateLimit, logger: logger}
}

func (r *DuplicateResolver) Run(ctx context.Context) error {
	for {
		jobs, err := r.store.ClaimDuplicateResolutionJobs(ctx, r.workers)
		if err != nil {
			return fmt.Errorf("claim duplicate resolution jobs: %w", err)
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
				r.process(ctx, job)
			}()
		}
		wait.Wait()
	}
}

func (r *DuplicateResolver) process(ctx context.Context, job domain.DuplicateResolutionJob) {
	candidates, err := r.store.DuplicateCandidates(ctx, job.Evidence, r.candidateLimit)
	if err != nil {
		r.retry(ctx, job, fmt.Errorf("load duplicate candidates: %w", err))
		return
	}
	decisions := make([]domain.DuplicateDecision, 0, len(candidates))
	accepted := 0
	for _, candidate := range candidates {
		decision := identity.EvaluateDuplicate(job.Evidence, candidate)
		decisions = append(decisions, decision)
		if decision.Status == domain.DuplicateAccepted {
			accepted++
		}
	}
	if err := r.store.CompleteDuplicateResolutionJob(ctx, job, decisions); err != nil {
		r.retry(ctx, job, fmt.Errorf("complete duplicate resolution: %w", err))
		return
	}
	r.logger.Info("duplicate resolution completed", "listing_id", job.Evidence.Listing.ID, "snapshot_id", job.Evidence.SnapshotID, "candidates", len(candidates), "accepted", accepted, "rule_version", identity.DuplicateRuleVersion, "mode", "shadow")
}

func (r *DuplicateResolver) retry(ctx context.Context, job domain.DuplicateResolutionJob, cause error) {
	r.logger.Warn("duplicate resolution failed", "listing_id", job.Evidence.Listing.ID, "snapshot_id", job.Evidence.SnapshotID, "error", cause)
	if err := r.store.RetryDuplicateResolutionJob(ctx, job, cause); err != nil {
		r.logger.Error("duplicate resolution retry scheduling failed", "snapshot_id", job.Evidence.SnapshotID, "error", err)
	}
}
