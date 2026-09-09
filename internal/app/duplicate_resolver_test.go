package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

type duplicateResolverStoreStub struct {
	candidates   []domain.ListingEvidence
	candidateErr error
	completeErr  error
	completed    []domain.DuplicateDecision
	retried      error
	limit        int
}

func (s *duplicateResolverStoreStub) ClaimDuplicateResolutionJobs(context.Context, int) ([]domain.DuplicateResolutionJob, error) {
	return nil, nil
}

func (s *duplicateResolverStoreStub) DuplicateCandidates(_ context.Context, _ domain.ListingEvidence, limit int) ([]domain.ListingEvidence, error) {
	s.limit = limit
	return s.candidates, s.candidateErr
}

func (s *duplicateResolverStoreStub) CompleteDuplicateResolutionJob(_ context.Context, _ domain.DuplicateResolutionJob, decisions []domain.DuplicateDecision) error {
	s.completed = decisions
	return s.completeErr
}

func (s *duplicateResolverStoreStub) RetryDuplicateResolutionJob(_ context.Context, _ domain.DuplicateResolutionJob, cause error) error {
	s.retried = cause
	return nil
}

func TestDuplicateResolverCompletesEvaluatedCandidates(t *testing.T) {
	current := resolverEvidence(1, "a")
	candidate := resolverEvidence(2, "b")
	store := &duplicateResolverStoreStub{candidates: []domain.ListingEvidence{candidate}}
	resolver := NewDuplicateResolver(store, 2, 17, slog.New(slog.NewTextHandler(io.Discard, nil)))

	resolver.process(context.Background(), domain.DuplicateResolutionJob{Evidence: current, AttemptID: 5})

	if store.retried != nil || len(store.completed) != 1 || store.limit != 17 {
		t.Fatalf("completed=%+v retried=%v limit=%d", store.completed, store.retried, store.limit)
	}
	if store.completed[0].Status != domain.DuplicateAccepted {
		t.Fatalf("decision = %+v", store.completed[0])
	}
}

func TestDuplicateResolverRetriesCandidateAndCompletionFailures(t *testing.T) {
	job := domain.DuplicateResolutionJob{Evidence: resolverEvidence(1, "a"), AttemptID: 5}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	candidateFailure := &duplicateResolverStoreStub{candidateErr: errors.New("candidate query failed")}
	NewDuplicateResolver(candidateFailure, 1, 10, logger).process(context.Background(), job)
	if candidateFailure.retried == nil || candidateFailure.completed != nil {
		t.Fatalf("candidate failure completed=%+v retried=%v", candidateFailure.completed, candidateFailure.retried)
	}

	completionFailure := &duplicateResolverStoreStub{completeErr: errors.New("commit failed")}
	NewDuplicateResolver(completionFailure, 1, 10, logger).process(context.Background(), job)
	if completionFailure.retried == nil {
		t.Fatal("completion failure was not retried")
	}
}

func resolverEvidence(id int64, externalID string) domain.ListingEvidence {
	rooms := 2
	area := 52.0
	return domain.ListingEvidence{
		SnapshotID: id * 10,
		Listing: domain.Listing{
			ID:                    id,
			Source:                "ss.lv",
			ExternalID:            externalID,
			PropertyType:          domain.PropertyApartment,
			DealType:              domain.DealRent,
			AreaKey:               "lv/riga/centrs",
			NormalizedAddress:     "brivibas iela 48",
			NormalizedDescription: "sunny renovated apartment",
			Rooms:                 &rooms,
			AreaM2:                &area,
		},
		Photos: []domain.PhotoFingerprint{{
			ExactHash:           "same",
			ExactAlgorithm:      "sha256-v1",
			PerceptualHash:      "0000000000000000",
			PerceptualAlgorithm: "dhash-64-v1",
		}},
	}
}
