package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/store/postgres/sqlcgen"
)

const staleAvailabilityClaim = 10 * time.Minute

func (s *Store) ClaimAvailabilityJobs(ctx context.Context, limit int, staleAfter time.Duration) ([]domain.AvailabilityJob, error) {
	if limit <= 0 {
		return nil, nil
	}
	if staleAfter <= 0 {
		staleAfter = 24 * time.Hour
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	queries := s.queries.WithTx(tx)
	if err := queries.ExpireStaleAvailabilityAttempts(ctx, sqlcgen.ExpireStaleAvailabilityAttemptsParams{
		ExpiredAt:    requiredTimestamptz(now),
		StaleSeconds: int(staleAvailabilityClaim / time.Second),
	}); err != nil {
		return nil, err
	}
	if err := queries.ReleaseStaleAvailabilityJobs(ctx, sqlcgen.ReleaseStaleAvailabilityJobsParams{
		ReleasedAt:   requiredTimestamptz(now),
		StaleSeconds: int(staleAvailabilityClaim / time.Second),
	}); err != nil {
		return nil, err
	}
	listingIDs, err := queries.ClaimableAvailabilityListingIDs(ctx, sqlcgen.ClaimableAvailabilityListingIDsParams{
		StaleSeconds: int(staleAfter / time.Second),
		JobLimit:     int32(limit),
	})
	if err != nil {
		return nil, err
	}

	result := make([]domain.AvailabilityJob, 0, len(listingIDs))
	for _, listingID := range listingIDs {
		if _, err := tx.Exec(ctx, `UPDATE listing_availability_jobs SET status='processing',claimed_at=$1,updated_at=$1 WHERE listing_id=$2`, now, listingID); err != nil {
			return nil, err
		}
		var attemptID int64
		if err := tx.QueryRow(ctx, `INSERT INTO listing_availability_attempts(listing_id,started_at) VALUES($1,$2) RETURNING id`, listingID, now).Scan(&attemptID); err != nil {
			return nil, err
		}
		listing, err := loadListing(ctx, tx, listingID)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.AvailabilityJob{Listing: listing, AttemptID: attemptID})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) CompleteAvailabilityJob(ctx context.Context, job domain.AvailabilityJob, observation domain.AvailabilityObservation, interval time.Duration) error {
	if !observation.Status.Valid() || observation.Evidence == "" {
		return fmt.Errorf("invalid availability observation")
	}
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var previous string
	var attempts int
	if err := tx.QueryRow(ctx, `SELECT l.availability_status,j.attempts FROM listings l JOIN listing_availability_jobs j ON j.listing_id=l.id WHERE l.id=$1 FOR UPDATE OF l,j`, job.Listing.ID).Scan(&previous, &attempts); err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE listings
		SET availability_status = $1,
		    last_seen_at = CASE WHEN $1 = 'active' THEN $2 ELSE last_seen_at END,
		    availability_changed_at = CASE
		        WHEN availability_status <> $1 THEN $2
		        ELSE availability_changed_at
		    END
		WHERE id = $3
	`, observation.Status, now, job.Listing.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO listing_availability_observations (
			listing_id, status, previous_status, observed_at,
			observation_kind, evidence, is_transition
		)
		VALUES ($1, $2, $3, $4, 'provider_check', $5, $3 <> $2)
	`, job.Listing.ID, observation.Status, previous, now, observation.Evidence); err != nil {
		return err
	}
	nextAttempt := now.Add(availabilityActiveDelay(interval))
	nextAttempts := 0
	if observation.Status == domain.AvailabilityUnknown {
		nextAttempt = now.Add(availabilityUnknownDelay(interval, attempts))
		nextAttempts = attempts + 1
	}
	if _, err := tx.Exec(ctx, `
		UPDATE listing_availability_jobs
		SET status = 'pending',
		    attempts = $1,
		    next_attempt_at = $2,
		    claimed_at = NULL,
		    last_error = NULL,
		    priority_requested = FALSE,
		    updated_at = $3
		WHERE listing_id = $4
	`, nextAttempts, nextAttempt, now, job.Listing.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE listing_availability_attempts SET finished_at=$1,outcome=$2,evidence=$3,error=NULL WHERE id=$4 AND outcome='processing'`, now, observation.Status, observation.Evidence, job.AttemptID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func availabilityUnknownDelay(base time.Duration, attempts int) time.Duration {
	if base <= 0 {
		base = 24 * time.Hour
	}
	delay := base
	for range min(attempts, 5) {
		delay *= 2
	}
	const maximum = 30 * 24 * time.Hour
	if delay > maximum {
		return maximum
	}
	return delay
}

func availabilityActiveDelay(staleAfter time.Duration) time.Duration {
	const weekly = 7 * 24 * time.Hour
	if staleAfter > weekly {
		return staleAfter
	}
	return weekly
}

func (s *Store) RetryAvailabilityJob(ctx context.Context, job domain.AvailabilityJob, cause error) error {
	message := cause.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var attempts int
	if err := tx.QueryRow(ctx, `SELECT attempts FROM listing_availability_jobs WHERE listing_id=$1 FOR UPDATE`, job.Listing.ID).Scan(&attempts); err != nil {
		return err
	}
	delay := 30 * (1 << min(attempts, 6))
	if delay > 3600 {
		delay = 3600
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE listing_availability_jobs
		SET status = 'pending',
		    attempts = attempts + 1,
		    next_attempt_at = $1,
		    claimed_at = NULL,
		    last_error = $2,
		    updated_at = $3
		WHERE listing_id = $4
	`, now.Add(time.Duration(delay)*time.Second), message, now, job.Listing.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE listing_availability_attempts SET finished_at=$1,outcome='failed',error=$2 WHERE id=$3 AND outcome='processing'`, now, message, job.AttemptID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CurrentPropertyOfferIDs(ctx context.Context, propertyID int64) ([]int64, error) {
	return s.propertyOfferIDs(ctx, propertyID, true)
}

func (s *Store) HistoricalPropertyOfferIDs(ctx context.Context, propertyID int64) ([]int64, error) {
	return s.propertyOfferIDs(ctx, propertyID, false)
}

func (s *Store) propertyOfferIDs(ctx context.Context, propertyID int64, activeOnly bool) ([]int64, error) {
	query := `SELECT m.listing_id FROM property_listing_memberships m JOIN listings l ON l.id=m.listing_id WHERE m.property_id=$1 AND m.valid_to IS NULL`
	if activeOnly {
		query += ` AND l.availability_status='active'`
	}
	query += ` ORDER BY m.listing_id`
	rows, err := s.pool.Query(ctx, query, propertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []int64
	for rows.Next() {
		var listingID int64
		if err := rows.Scan(&listingID); err != nil {
			return nil, err
		}
		result = append(result, listingID)
	}
	return result, rows.Err()
}
