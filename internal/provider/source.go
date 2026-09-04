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
