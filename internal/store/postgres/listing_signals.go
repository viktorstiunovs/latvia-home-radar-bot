package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/store/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
)

const staleSignalClaim = 10 * time.Minute

func (s *Store) ClaimListingSignalJobs(ctx context.Context, limit int) ([]domain.ListingSignalJob, error) {
	if limit <= 0 {
		return nil, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)
	candidates, err := queries.ClaimableListingSignalJobs(ctx, sqlcgen.ClaimableListingSignalJobsParams{
		StaleSeconds: int(staleSignalClaim / time.Second),
		JobLimit:     int32(limit),
	})
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	result := make([]domain.ListingSignalJob, 0, len(candidates))
	for _, candidate := range candidates {
		_, err := tx.Exec(ctx, `UPDATE listing_signal_attempts SET finished_at=$1,outcome='failed',error='worker claim expired' WHERE listing_id=$2 AND generation=$3 AND outcome='processing'`, now, candidate.ListingID, candidate.Generation)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE listing_signal_jobs SET status='processing',claimed_at=$1,updated_at=$1 WHERE listing_id=$2 AND generation=$3`, now, candidate.ListingID, candidate.Generation); err != nil {
			return nil, err
		}
		var attemptID int64
		if err := tx.QueryRow(ctx, `INSERT INTO listing_signal_attempts(listing_id,generation,started_at) VALUES($1,$2,$3) RETURNING id`, candidate.ListingID, candidate.Generation, now).Scan(&attemptID); err != nil {
			return nil, err
		}
		listing, err := loadListing(ctx, tx, candidate.ListingID)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.ListingSignalJob{Listing: listing, Generation: candidate.Generation, AttemptID: attemptID})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) ReusablePhotoFingerprints(ctx context.Context, listingID int64) ([]domain.PhotoFingerprint, error) {
	rows, err := s.queries.ListReusablePhotoFingerprints(ctx, listingID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.PhotoFingerprint, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.PhotoFingerprint{
			Position:            int(row.Position),
			SourceURL:           row.SourceURL,
			ExactHash:           row.ExactHash,
			ExactAlgorithm:      row.ExactAlgorithm,
			PerceptualHash:      row.PerceptualHash,
			PerceptualAlgorithm: row.PerceptualAlgorithm,
			MediaType:           row.MediaType,
			ByteSize:            row.ByteSize,
			Width:               row.Width,
			Height:              row.Height,
		})
	}
	return result, nil
}

func (s *Store) CompleteListingSignalJob(ctx context.Context, job domain.ListingSignalJob, signals domain.ListingSignals) error {
	if signals.Availability != nil && (!signals.Availability.Status.Valid() || signals.Availability.Evidence == "") {
		return fmt.Errorf("invalid signal availability observation")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	currentGeneration, err := signalJobGeneration(ctx, tx, job.Listing.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if currentGeneration != job.Generation {
		if _, err := tx.Exec(ctx, `UPDATE listing_signal_attempts SET finished_at=$1,outcome='superseded' WHERE id=$2 AND outcome='processing'`, now, job.AttemptID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if signals.Availability != nil {
		var previous string
		if err := tx.QueryRow(ctx, `SELECT availability_status FROM listings WHERE id=$1 FOR UPDATE`, job.Listing.ID).Scan(&previous); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE listings SET availability_status=$1,availability_changed_at=CASE WHEN availability_status<>$1 THEN $2 ELSE availability_changed_at END WHERE id=$3`, signals.Availability.Status, now, job.Listing.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO listing_availability_observations (
				listing_id, status, previous_status, observed_at,
				observation_kind, evidence, is_transition
			)
			VALUES ($1, $2, $3, $4, 'provider_check', $5, $3 <> $2)
		`, job.Listing.ID, signals.Availability.Status, previous, now, signals.Availability.Evidence); err != nil {
			return err
		}
	}

	photoURLs := signals.Listing.PhotoURLs
	if photoURLs == nil {
		photoURLs = []string{}
	}
	photoJSON, err := json.Marshal(photoURLs)
	if err != nil {
		return err
	}
	factsJSON, err := json.Marshal(structuredFacts(signals.Listing))
	if err != nil {
		return err
	}
	queries := s.queries.WithTx(tx)
	err = queries.UpdateListingSignals(ctx, sqlcgen.UpdateListingSignalsParams{
		Description:           signals.Listing.Description,
		NormalizedAddress:     signals.Listing.NormalizedAddress,
		NormalizedDescription: signals.Listing.NormalizedDescription,
		Address:               signals.Listing.Address,
		Rooms:                 signals.Listing.Rooms,
		AreaM2:                signals.Listing.AreaM2,
		Floor:                 signals.Listing.Floor,
		TotalFloors:           signals.Listing.TotalFloors,
		BuildingSeries:        signals.Listing.BuildingSeries,
		BuildingType:          signals.Listing.BuildingType,
		LandAreaM2:            signals.Listing.LandAreaM2,
		ImageURL:              signals.Listing.ImageURL,
		PhotoURLs:             photoJSON,
		DetailsEnriched:       signals.Listing.DetailsEnriched,
		ListingID:             job.Listing.ID,
	})
	if err != nil {
		return err
	}
	snapshotID, err := queries.InsertListingSignalSnapshot(ctx, sqlcgen.InsertListingSignalSnapshotParams{
		ListingID:             job.Listing.ID,
		InputHash:             signals.InputHash,
		NormalizationVersion:  signals.NormalizationVersion,
		Description:           signals.Listing.Description,
		NormalizedAddress:     signals.Listing.NormalizedAddress,
		NormalizedDescription: signals.Listing.NormalizedDescription,
		StructuredFacts:       factsJSON,
		PhotoURLs:             photoJSON,
		CompletedAt:           requiredTimestamptz(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT id FROM listing_signal_snapshots WHERE listing_id=$1 AND input_hash=$2 AND normalization_version=$3`, job.Listing.ID, signals.InputHash, signals.NormalizationVersion).Scan(&snapshotID)
	}
	if err != nil {
		return err
	}
	for _, photo := range signals.Photos {
		err := queries.InsertPhotoFingerprint(ctx, sqlcgen.InsertPhotoFingerprintParams{
			SnapshotID:          snapshotID,
			ListingID:           job.Listing.ID,
			Position:            int16(photo.Position),
			SourceURL:           photo.SourceURL,
			ExactHash:           photo.ExactHash,
			ExactAlgorithm:      photo.ExactAlgorithm,
			PerceptualHash:      photo.PerceptualHash,
			PerceptualAlgorithm: photo.PerceptualAlgorithm,
			MediaType:           photo.MediaType,
			ByteSize:            photo.ByteSize,
			Width:               photo.Width,
			Height:              photo.Height,
			CreatedAt:           requiredTimestamptz(now),
		})
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO duplicate_resolution_jobs(snapshot_id,next_attempt_at,created_at,updated_at) VALUES($1,now(),$2,$2) ON CONFLICT(snapshot_id) DO NOTHING`, snapshotID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE listing_signal_jobs SET status='completed',attempts=attempts+1,claimed_at=NULL,last_error=NULL,updated_at=$1 WHERE listing_id=$2 AND generation=$3`, now, job.Listing.ID, job.Generation); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE listing_signal_attempts SET finished_at=$1,outcome='completed',error=NULL WHERE id=$2 AND outcome='processing'`, now, job.AttemptID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RetryListingSignalJob(ctx context.Context, job domain.ListingSignalJob, cause error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	currentGeneration, err := signalJobGeneration(ctx, tx, job.Listing.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if currentGeneration != job.Generation {
		if _, err := tx.Exec(ctx, `UPDATE listing_signal_attempts SET finished_at=$1,outcome='superseded' WHERE id=$2 AND outcome='processing'`, now, job.AttemptID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	message := cause.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	var attempts int
	if err := tx.QueryRow(ctx, `SELECT attempts FROM listing_signal_jobs WHERE listing_id=$1`, job.Listing.ID).Scan(&attempts); err != nil {
		return err
	}
	delay := 5 * (1 << min(attempts, 6))
	if delay > 300 {
		delay = 300
	}
	if _, err := tx.Exec(ctx, `
		UPDATE listing_signal_jobs
		SET status = 'pending',
		    attempts = attempts + 1,
		    next_attempt_at = $1,
		    claimed_at = NULL,
		    last_error = $2,
		    updated_at = $3
		WHERE listing_id = $4 AND generation = $5
	`, now.Add(time.Duration(delay)*time.Second), message, now, job.Listing.ID, job.Generation); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE listing_signal_attempts SET finished_at=$1,outcome='failed',error=$2 WHERE id=$3 AND outcome='processing'`, now, message, job.AttemptID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func signalJobGeneration(ctx context.Context, tx pgx.Tx, listingID int64) (int64, error) {
	var generation int64
	err := tx.QueryRow(ctx, `SELECT generation FROM listing_signal_jobs WHERE listing_id=$1 FOR UPDATE`, listingID).Scan(&generation)
	return generation, err
}

func structuredFacts(listing domain.Listing) map[string]any {
	return map[string]any{
		"property_type":   listing.PropertyType,
		"deal_type":       listing.DealType,
		"area_key":        listing.AreaKey,
		"city":            listing.City,
		"district":        listing.District,
		"rooms":           listing.Rooms,
		"area_m2":         listing.AreaM2,
		"floor":           listing.Floor,
		"total_floors":    listing.TotalFloors,
		"building_series": listing.BuildingSeries,
		"building_type":   listing.BuildingType,
		"land_area_m2":    listing.LandAreaM2,
	}
}
