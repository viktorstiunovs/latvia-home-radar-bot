package app

import (
	"context"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/events"
)

type ListingStore interface {
	IsSourceInitialized(context.Context, string) (bool, error)
	EnrichmentKeys(context.Context, []domain.Listing) (map[domain.ListingKey]struct{}, error)
	ProcessDiscovered(context.Context, string, []domain.Listing, bool) (domain.DiscoveryResult, error)
	RecordFeedSightings(context.Context, []domain.Listing) error
}

type OutboxStore interface {
	PendingOutbox(context.Context, int) ([]events.Envelope, error)
	MarkOutboxPublished(context.Context, string) error
	RetryOutbox(context.Context, string, error) error
}

type ListingMatcherStore interface {
	MatchListingEvent(context.Context, string, int64, time.Time) (int, error)
	MatchDuplicateAwareListingEvent(context.Context, string, int64, time.Time) (int, error)
	MatchPriceChangedEvent(context.Context, string, events.ListingPriceChanged, time.Time) (int, error)
}

type EventPublisher interface {
	Publish(context.Context, events.Envelope) error
}

type EventConsumer interface {
	Consume(context.Context, func(context.Context, events.Envelope) error) error
}

type NotificationStore interface {
	Pending(context.Context, int) ([]domain.PendingNotification, error)
	MarkSent(context.Context, int64) error
	Retry(context.Context, int64, int, error, time.Duration) error
	Fail(context.Context, int64, error) error
	DisableFiltersForChat(context.Context, int64) (int64, error)
	PhotoFileIDs(context.Context, int64) ([]string, error)
	CachePhotoFileIDs(context.Context, int64, []string) error
	ClearPhotoFileIDs(context.Context, int64) error
}

type ListingSignalStore interface {
	ClaimListingSignalJobs(context.Context, int) ([]domain.ListingSignalJob, error)
	ReusablePhotoFingerprints(context.Context, int64) ([]domain.PhotoFingerprint, error)
	CompleteListingSignalJob(context.Context, domain.ListingSignalJob, domain.ListingSignals) error
	RetryListingSignalJob(context.Context, domain.ListingSignalJob, error) error
}

type DuplicateResolverStore interface {
	ClaimDuplicateResolutionJobs(context.Context, int) ([]domain.DuplicateResolutionJob, error)
	DuplicateCandidates(context.Context, domain.ListingEvidence, int) ([]domain.ListingEvidence, error)
	CompleteDuplicateResolutionJob(context.Context, domain.DuplicateResolutionJob, []domain.DuplicateDecision) error
	RetryDuplicateResolutionJob(context.Context, domain.DuplicateResolutionJob, error) error
}

type AvailabilityStore interface {
	ClaimAvailabilityJobs(context.Context, int, time.Duration) ([]domain.AvailabilityJob, error)
	CompleteAvailabilityJob(context.Context, domain.AvailabilityJob, domain.AvailabilityObservation, time.Duration) error
	RetryAvailabilityJob(context.Context, domain.AvailabilityJob, error) error
}
