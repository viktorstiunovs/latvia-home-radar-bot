package provider

import (
	"context"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

type Source interface {
	Key() string
	FetchRecent(context.Context) ([]domain.Listing, error)
	Enrich(context.Context, domain.Listing) (domain.Listing, error)
}

type AvailabilityChecker interface {
	CheckAvailability(context.Context, domain.Listing) (domain.AvailabilityObservation, error)
}

type ListingUnavailableError struct {
	Evidence string
}

func (e *ListingUnavailableError) Error() string {
	return e.Evidence
}

// PartialEnrichmentError reports that a provider returned a safe listing
// snapshot but could not complete every optional detail lookup. Callers may
// persist the returned listing while leaving its detail enrichment retryable.
type PartialEnrichmentError struct {
	Err error
}

func (e *PartialEnrichmentError) Error() string {
	return e.Err.Error()
}

func (e *PartialEnrichmentError) Unwrap() error {
	return e.Err
}
