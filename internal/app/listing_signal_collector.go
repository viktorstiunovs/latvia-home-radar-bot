package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/identity"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
)

type ListingSignalCollector struct {
	store      ListingSignalStore
	sources    map[string]provider.Source
	http       *http.Client
	workers    int
	photoLimit int
	logger     *slog.Logger
}

func NewListingSignalCollector(store ListingSignalStore, sources []provider.Source, httpClient *http.Client, workers, photoLimit int, logger *slog.Logger) *ListingSignalCollector {
	if workers < 1 {
		workers = 1
	}
	if photoLimit < 1 || photoLimit > identity.DefaultPhotoLimit {
		photoLimit = identity.DefaultPhotoLimit
	}
	indexed := make(map[string]provider.Source, len(sources))
	for _, source := range sources {
		indexed[source.Key()] = source
	}
	return &ListingSignalCollector{store: store, sources: indexed, http: httpClient, workers: workers, photoLimit: photoLimit, logger: logger}
}

func (c *ListingSignalCollector) Run(ctx context.Context) error {
	for {
		jobs, err := c.store.ClaimListingSignalJobs(ctx, c.workers)
		if err != nil {
			return fmt.Errorf("claim listing signal jobs: %w", err)
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
				c.process(ctx, job)
			}()
		}
		wait.Wait()
	}
}

func (c *ListingSignalCollector) process(ctx context.Context, job domain.ListingSignalJob) {
	source := c.sources[listingSourceKey(job.Listing)]
	if source == nil {
		c.retry(ctx, job, fmt.Errorf("no provider source for %s", listingSourceKey(job.Listing)))
		return
	}
	enriched := job.Listing
	if !enriched.DetailsEnriched || enriched.Description == "" {
		var err error
		enriched, err = source.Enrich(ctx, job.Listing)
		if err != nil {
			c.retry(ctx, job, fmt.Errorf("enrich listing: %w", err))
			return
		}
	}
	if enriched.Description == "" {
		enriched.Description = job.Listing.Description
	}
	enriched.NormalizedAddress = identity.NormalizeAddress(enriched.Address)
	enriched.NormalizedDescription = identity.NormalizeDescription(enriched.Description)
	photoURLs := enriched.PhotoURLs
	if len(photoURLs) == 0 && enriched.ImageURL != "" {
		photoURLs = []string{enriched.ImageURL}
	}
	reusable, err := c.store.ReusablePhotoFingerprints(ctx, job.Listing.ID)
	if err != nil {
		c.retry(ctx, job, fmt.Errorf("load reusable photo fingerprints: %w", err))
		return
	}
	photos, err := identity.FingerprintPhotosWithReuse(ctx, c.http, enriched.Source, photoURLs, c.photoLimit, reusable)
	if err != nil {
		c.retry(ctx, job, err)
		return
	}
	signals := domain.ListingSignals{
		Listing:              enriched,
		InputHash:            identity.SignalInputHash(enriched, photoURLs),
		NormalizationVersion: identity.NormalizationVersion,
		Photos:               photos,
	}
	if err := c.store.CompleteListingSignalJob(ctx, job, signals); err != nil {
		c.retry(ctx, job, fmt.Errorf("complete listing signals: %w", err))
		return
	}
	c.logger.Info("listing signals collected", "listing_id", job.Listing.ID, "listing_source", job.Listing.Source, "listing_external_id", job.Listing.ExternalID, "photos", len(photos), "generation", job.Generation)
}

func (c *ListingSignalCollector) retry(ctx context.Context, job domain.ListingSignalJob, cause error) {
	c.logger.Warn("listing signal collection failed", "listing_id", job.Listing.ID, "listing_source", job.Listing.Source, "listing_external_id", job.Listing.ExternalID, "generation", job.Generation, "error", cause)
	if err := c.store.RetryListingSignalJob(ctx, job, cause); err != nil {
		c.logger.Error("listing signal retry scheduling failed", "listing_id", job.Listing.ID, "generation", job.Generation, "error", err)
	}
}

func listingSourceKey(listing domain.Listing) string {
	return fmt.Sprintf("%s:latvia:%ss:%s", listing.Source, listing.PropertyType, listing.DealType)
}
