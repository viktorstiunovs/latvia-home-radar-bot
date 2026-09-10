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
