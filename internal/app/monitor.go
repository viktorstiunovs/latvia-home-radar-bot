package app

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
)

type Monitor struct {
	store       ListingStore
	sources     []provider.Source
	interval    time.Duration
	detailDelay time.Duration
	logger      *slog.Logger
}

func NewMonitor(store ListingStore, sources []provider.Source, interval time.Duration, logger *slog.Logger) *Monitor {
	return &Monitor{store: store, sources: sources, interval: interval, detailDelay: 500 * time.Millisecond, logger: logger}
}

func (m *Monitor) Run(ctx context.Context) error {
	for {
		for _, source := range m.sources {
			if err := m.Poll(ctx, source); err != nil {
				m.logger.Error("source poll failed", "source", source.Key(), "error", err)
			}
		}
		jitter := (rand.Float64()*.2 - .1) * float64(m.interval)
		delay := m.interval + time.Duration(jitter)
		if delay < 30*time.Second {
			delay = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (m *Monitor) Poll(ctx context.Context, source provider.Source) error {
	initialized, err := m.store.IsSourceInitialized(ctx, source.Key())
	if err != nil {
		return err
	}
	m.logger.Info("polling source", "source", source.Key())
	listings, err := source.FetchRecent(ctx)
	if err != nil {
		return err
	}
	parsed := len(listings)
	sightings := listings
	if initialized {
		needed, err := m.store.EnrichmentKeys(ctx, listings)
		if err != nil {
			return err
		}
		listings = m.enrich(ctx, source, listings, needed)
	}
	result, err := m.store.ProcessDiscovered(ctx, source.Key(), listings, initialized)
	if err != nil {
		return err
	}
	if err := m.store.RecordFeedSightings(ctx, sightings); err != nil {
		return err
	}
	m.logger.Info("source poll complete", "source", source.Key(), "parsed", parsed, "new", result.Inserted, "events", result.Events, "baseline", !initialized)
	return nil
}

func (m *Monitor) enrich(ctx context.Context, source provider.Source, listings []domain.Listing, needed map[domain.ListingKey]struct{}) []domain.Listing {
	targets := make([]domain.Listing, 0, len(needed))
	for _, l := range listings {
		if _, ok := needed[l.Key()]; ok {
			targets = append(targets, l)
		}
	}
	result := make([]domain.Listing, 0, len(targets))
	for index, listing := range targets {
		enriched, err := source.Enrich(ctx, listing)
		if err != nil {
			m.logger.Warn("could not enrich listing", "source", source.Key(), "listing", listing.ExternalID, "error", err)
		} else if enriched.DetailsEnriched {
			result = append(result, enriched)
		}
		if index+1 < len(targets) {
			select {
			case <-ctx.Done():
				return result
			case <-time.After(m.detailDelay):
			}
		}
	}
	m.logger.Info("listing enrichment complete", "source", source.Key(), "requested", len(targets), "enriched", len(result))
	return result
}
