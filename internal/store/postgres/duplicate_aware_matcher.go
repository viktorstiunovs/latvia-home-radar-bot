package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/events"
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
		tag, err := tx.Exec(ctx, `INSERT INTO notifications(filter_id,listing_id,trigger_event_id,notification_type,previous_price_eur,current_price_eur,property_id,comparison_listing_id,listing_snapshot,active_alternatives,status,next_attempt_at) VALUES($1,$2,$3::uuid,$4,$5,$6,$7,$8,$9,$10,'pending',now()) ON CONFLICT DO NOTHING`, filter.ID, listingID, eventID, decision.Type, decision.PreviousPriceEUR, listing.PriceEUR, propertyID, decision.ComparisonListingID, listingJSON, alternativesJSON)
		if err != nil {
			return 0, err
		}
		created += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return created, nil
}

func prepareResolvedProperty(ctx context.Context, tx pgx.Tx, listingID int64, occurredAt time.Time) (int64, string, error) {
	var propertyID int64
	err := tx.QueryRow(ctx, `SELECT m.property_id FROM property_listing_memberships m WHERE m.listing_id=$1 AND m.valid_to IS NULL AND EXISTS (SELECT 1 FROM listing_signal_snapshots s JOIN duplicate_resolution_jobs j ON j.snapshot_id=s.id AND j.status='completed' WHERE s.listing_id=$1)`, listingID).Scan(&propertyID)
	if err == pgx.ErrNoRows {
		return 0, fmt.Sprintf("property resolution pending for listing %d", listingID), nil
	}
	if err != nil {
		return 0, "", err
	}

	var missingAvailability int
	err = tx.QueryRow(ctx, `WITH missing AS MATERIALIZED (SELECT other.listing_id FROM property_listing_memberships other JOIN listings other_listing ON other_listing.id=other.listing_id WHERE other.property_id=$1 AND other.valid_to IS NULL AND other.listing_id<>$2 AND other_listing.availability_status<>'inactive' AND NOT EXISTS (SELECT 1 FROM listing_availability_observations o WHERE o.listing_id=other.listing_id AND o.observation_kind='provider_check' AND o.observed_at>=$3)), scheduled AS (INSERT INTO listing_availability_jobs(listing_id,next_attempt_at,priority_requested,created_at,updated_at) SELECT listing_id,now(),TRUE,now(),now() FROM missing ON CONFLICT(listing_id) DO UPDATE SET next_attempt_at=CASE WHEN listing_availability_jobs.status='pending' THEN least(listing_availability_jobs.next_attempt_at,excluded.next_attempt_at) ELSE listing_availability_jobs.next_attempt_at END,priority_requested=CASE WHEN listing_availability_jobs.status='pending' THEN TRUE ELSE listing_availability_jobs.priority_requested END,updated_at=CASE WHEN listing_availability_jobs.status='pending' THEN excluded.updated_at ELSE listing_availability_jobs.updated_at END RETURNING listing_id) SELECT count(*) FROM missing`, propertyID, listingID, occurredAt).Scan(&missingAvailability)
	if err != nil {
		return 0, "", err
	}
	if missingAvailability > 0 {
		return propertyID, fmt.Sprintf("availability evidence pending for %d alternative listings in property %d", missingAvailability, propertyID), nil
	}
	return propertyID, "", nil
}

func propertyOffersAt(ctx context.Context, tx pgx.Tx, propertyID, excludeListingID int64, occurredAt time.Time) ([]propertyOfferAtEvent, error) {
	rows, err := tx.Query(ctx, `SELECT l.id,l.source,l.url,event_price.price_eur,last_known.price_eur,l.availability_status,l.first_seen_at FROM property_listing_memberships m JOIN listings l ON l.id=m.listing_id LEFT JOIN LATERAL (SELECT h.price_eur FROM listing_price_history h WHERE h.listing_id=l.id AND h.observed_at<=$3 ORDER BY h.observed_at DESC,h.id DESC LIMIT 1) event_price ON TRUE LEFT JOIN LATERAL (SELECT h.price_eur FROM listing_price_history h WHERE h.listing_id=l.id AND h.observed_at<=$3 AND h.price_eur IS NOT NULL ORDER BY h.observed_at DESC,h.id DESC LIMIT 1) last_known ON TRUE WHERE m.property_id=$1 AND m.valid_to IS NULL AND l.id<>$2 ORDER BY l.first_seen_at DESC,l.id DESC`, propertyID, excludeListingID, occurredAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []propertyOfferAtEvent
	for rows.Next() {
		var offer propertyOfferAtEvent
		if err := rows.Scan(&offer.alternative.ListingID, &offer.alternative.Source, &offer.alternative.URL, &offer.alternative.PriceEUR, &offer.lastKnownPrice, &offer.status, &offer.firstSeen); err != nil {
			return nil, err
		}
		result = append(result, offer)
	}
	return result, rows.Err()
}

func propertyWasNotified(ctx context.Context, tx pgx.Tx, propertyID, userID int64) (bool, error) {
	var result bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM notifications n JOIN filters f ON f.id=n.filter_id JOIN property_listing_memberships m ON m.listing_id=n.listing_id AND m.valid_to IS NULL WHERE f.user_id=$1 AND m.property_id=$2)`, userID, propertyID).Scan(&result)
	return result, err
}
