package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/events"
	"github.com/clive00lewis/latvia-home-radar/internal/store/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
)

type propertyOfferAtEvent struct {
	alternative    domain.ListingAlternative
	status         domain.AvailabilityStatus
	lastKnownPrice *int
	firstSeen      time.Time
}

func (s *Store) MatchDuplicateAwareListingEvent(ctx context.Context, eventID string, listingID int64, occurredAt time.Time) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)

	propertyID, pendingReason, err := prepareResolvedProperty(ctx, tx, listingID, occurredAt)
	if err != nil {
		return 0, err
	}
	if pendingReason != "" {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, events.DependencyPending(fmt.Errorf("%s", pendingReason))
	}
	tag, err := tx.Exec(ctx, `INSERT INTO consumed_events(consumer_name,event_id,consumed_at) VALUES($1,$2::uuid,$3) ON CONFLICT DO NOTHING`, listingMatcherConsumer, eventID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, nil
	}

	listing, err := loadListing(ctx, tx, listingID)
	if err != nil {
		return 0, err
	}
	if err := tx.QueryRow(ctx, `SELECT price_eur FROM listing_price_history WHERE listing_id=$1 ORDER BY observed_at,id LIMIT 1`, listingID).Scan(&listing.PriceEUR); err != nil {
		return 0, err
	}
	if err := tx.QueryRow(ctx, `SELECT status FROM listing_availability_observations WHERE listing_id=$1 AND observed_at<=$2 ORDER BY observed_at DESC,id DESC LIMIT 1`, listingID, occurredAt).Scan(&listing.AvailabilityStatus); err != nil {
		return 0, err
	}

	offers, err := propertyOffersAt(ctx, tx, propertyID, listingID, occurredAt)
	if err != nil {
		return 0, err
	}
	activeAlternatives := make([]domain.ListingAlternative, 0, len(offers))
	var priorInactive *domain.ListingAlternative
	for _, offer := range offers {
		switch offer.status {
		case domain.AvailabilityActive:
			activeAlternatives = append(activeAlternatives, offer.alternative)
		case domain.AvailabilityInactive:
			if priorInactive == nil {
				candidate := offer.alternative
				candidate.PriceEUR = offer.lastKnownPrice
				priorInactive = &candidate
			}
		}
	}

	listingJSON, err := json.Marshal(listing)
	if err != nil {
		return 0, err
	}
	alternativesJSON, err := json.Marshal(activeAlternatives)
	if err != nil {
		return 0, err
	}
	filters, err := loadFilters(ctx, tx)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, filter := range matchingFiltersByUser(filters, listing, occurredAt) {
		alreadyNotified, err := propertyWasNotified(ctx, tx, propertyID, filter.UserID)
		if err != nil {
			return 0, err
		}
		decision := domain.ClassifyPropertyDiscovery(listing.PriceEUR, activeAlternatives, priorInactive, alreadyNotified)
		if !decision.Notify {
			continue
		}
		inserted, err := queries.InsertDuplicateAwareNotification(ctx, sqlcgen.InsertDuplicateAwareNotificationParams{
			FilterID:            filter.ID,
			ListingID:           listingID,
			EventID:             eventID,
			NotificationType:    string(decision.Type),
			PreviousPriceEUR:    decision.PreviousPriceEUR,
			CurrentPriceEUR:     listing.PriceEUR,
			PropertyID:          &propertyID,
			ComparisonListingID: decision.ComparisonListingID,
			ListingSnapshot:     listingJSON,
			ActiveAlternatives:  alternativesJSON,
		})
		if err != nil {
			return 0, err
		}
		created += int(inserted)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return created, nil
}

func prepareResolvedProperty(ctx context.Context, tx pgx.Tx, listingID int64, occurredAt time.Time) (int64, string, error) {
	queries := sqlcgen.New(tx)
	propertyID, err := queries.ResolvedPropertyIDForListing(ctx, listingID)
	if err == pgx.ErrNoRows {
		return 0, fmt.Sprintf("property resolution pending for listing %d", listingID), nil
	}
	if err != nil {
		return 0, "", err
	}

	missingAvailability, err := queries.ScheduleMissingPropertyAvailabilityChecks(ctx, sqlcgen.ScheduleMissingPropertyAvailabilityChecksParams{
		PropertyID:        propertyID,
		ExcludedListingID: listingID,
		ObservedAt:        requiredTimestamptz(occurredAt),
	})
	if err != nil {
		return 0, "", err
	}
	if missingAvailability > 0 {
		return propertyID, fmt.Sprintf("availability evidence pending for %d alternative listings in property %d", missingAvailability, propertyID), nil
	}
	return propertyID, "", nil
}

func propertyOffersAt(ctx context.Context, tx pgx.Tx, propertyID, excludeListingID int64, occurredAt time.Time) ([]propertyOfferAtEvent, error) {
	queries := sqlcgen.New(tx)
	rows, err := queries.ListOtherPropertyOffers(ctx, sqlcgen.ListOtherPropertyOffersParams{
		OccurredAt:        requiredTimestamptz(occurredAt),
		PropertyID:        propertyID,
		ExcludedListingID: excludeListingID,
	})
	if err != nil {
		return nil, err
	}
	result := make([]propertyOfferAtEvent, 0, len(rows))
	for _, row := range rows {
		offer := propertyOfferAtEvent{
			alternative: domain.ListingAlternative{
				ListingID: row.ID,
				Source:    row.Source,
				URL:       row.URL,
				PriceEUR:  row.EventPriceEUR,
			},
			status:         domain.AvailabilityStatus(row.AvailabilityStatus),
			lastKnownPrice: row.LastKnownPriceEUR,
			firstSeen:      row.FirstSeenAt.Time,
		}
		result = append(result, offer)
	}
	return result, nil
}

func propertyWasNotified(ctx context.Context, tx pgx.Tx, propertyID, userID int64) (bool, error) {
	return sqlcgen.New(tx).UserWasNotifiedAboutProperty(ctx, sqlcgen.UserWasNotifiedAboutPropertyParams{
		UserID:     userID,
		PropertyID: propertyID,
	})
}
