package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
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
	rows, err := tx.Query(ctx, `SELECT listing_id,generation FROM listing_signal_jobs WHERE (status='pending' AND next_attempt_at<=now()) OR (status='processing' AND claimed_at<now()-($1 * interval '1 second')) ORDER BY next_attempt_at,listing_id FOR UPDATE SKIP LOCKED LIMIT $2`, int(staleSignalClaim/time.Second), limit)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		listingID  int64
		generation int64
	}
	var candidates []candidate
	for rows.Next() {
		var value candidate
		if err := rows.Scan(&value.listingID, &value.generation); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	now := time.Now().UTC()
	result := make([]domain.ListingSignalJob, 0, len(candidates))
	for _, candidate := range candidates {
		_, err := tx.Exec(ctx, `UPDATE listing_signal_attempts SET finished_at=$1,outcome='failed',error='worker claim expired' WHERE listing_id=$2 AND generation=$3 AND outcome='processing'`, now, candidate.listingID, candidate.generation)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE listing_signal_jobs SET status='processing',claimed_at=$1,updated_at=$1 WHERE listing_id=$2 AND generation=$3`, now, candidate.listingID, candidate.generation); err != nil {
			return nil, err
		}
		var attemptID int64
		if err := tx.QueryRow(ctx, `INSERT INTO listing_signal_attempts(listing_id,generation,started_at) VALUES($1,$2,$3) RETURNING id`, candidate.listingID, candidate.generation, now).Scan(&attemptID); err != nil {
			return nil, err
		}
		listing, err := loadListing(ctx, tx, candidate.listingID)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.ListingSignalJob{Listing: listing, Generation: candidate.generation, AttemptID: attemptID})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) ReusablePhotoFingerprints(ctx context.Context, listingID int64) ([]domain.PhotoFingerprint, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (source_url) position,source_url,exact_hash,exact_algorithm,perceptual_hash,perceptual_algorithm,media_type,byte_size,width,height FROM listing_photo_fingerprints WHERE listing_id=$1 ORDER BY source_url,created_at DESC,id DESC`, listingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.PhotoFingerprint
	for rows.Next() {
		var value domain.PhotoFingerprint
		if err := rows.Scan(&value.Position, &value.SourceURL, &value.ExactHash, &value.ExactAlgorithm, &value.PerceptualHash, &value.PerceptualAlgorithm, &value.MediaType, &value.ByteSize, &value.Width, &value.Height); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
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
		if _, err := tx.Exec(ctx, `INSERT INTO listing_availability_observations(listing_id,status,previous_status,observed_at,observation_kind,evidence,is_transition) VALUES($1,$2,$3,$4,'provider_check',$5,$3<>$2)`, job.Listing.ID, signals.Availability.Status, previous, now, signals.Availability.Evidence); err != nil {
			return err
		}
	}

	photoJSON, err := json.Marshal(signals.Listing.PhotoURLs)
	if err != nil {
		return err
	}
	factsJSON, err := json.Marshal(structuredFacts(signals.Listing))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE listings SET description=$1,normalized_address=$2,normalized_description=$3,address=COALESCE(NULLIF($4,''),address),rooms=COALESCE($5,rooms),area_m2=COALESCE($6,area_m2),floor=COALESCE($7,floor),total_floors=COALESCE($8,total_floors),building_series=COALESCE(NULLIF($9,''),building_series),building_type=COALESCE(NULLIF($10,''),building_type),land_area_m2=COALESCE($11,land_area_m2),image_url=COALESCE(NULLIF($12,''),image_url),photo_urls=CASE WHEN jsonb_array_length($13::jsonb)>0 THEN $13::jsonb ELSE photo_urls END,details_enriched=details_enriched OR $14 WHERE id=$15`, signals.Listing.Description, signals.Listing.NormalizedAddress, signals.Listing.NormalizedDescription, signals.Listing.Address, signals.Listing.Rooms, signals.Listing.AreaM2, signals.Listing.Floor, signals.Listing.TotalFloors, signals.Listing.BuildingSeries, signals.Listing.BuildingType, signals.Listing.LandAreaM2, signals.Listing.ImageURL, photoJSON, signals.Listing.DetailsEnriched, job.Listing.ID)
	if err != nil {
		return err
	}
	var snapshotID int64
	err = tx.QueryRow(ctx, `INSERT INTO listing_signal_snapshots(listing_id,input_hash,normalization_version,description,normalized_address,normalized_description,structured_facts,photo_urls,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(listing_id,input_hash,normalization_version) DO NOTHING RETURNING id`, job.Listing.ID, signals.InputHash, signals.NormalizationVersion, signals.Listing.Description, signals.Listing.NormalizedAddress, signals.Listing.NormalizedDescription, factsJSON, photoJSON, now).Scan(&snapshotID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT id FROM listing_signal_snapshots WHERE listing_id=$1 AND input_hash=$2 AND normalization_version=$3`, job.Listing.ID, signals.InputHash, signals.NormalizationVersion).Scan(&snapshotID)
	}
	if err != nil {
		return err
	}
	for _, photo := range signals.Photos {
		_, err := tx.Exec(ctx, `INSERT INTO listing_photo_fingerprints(snapshot_id,listing_id,position,source_url,exact_hash,exact_algorithm,perceptual_hash,perceptual_algorithm,media_type,byte_size,width,height,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(snapshot_id,position) DO NOTHING`, snapshotID, job.Listing.ID, photo.Position, photo.SourceURL, photo.ExactHash, photo.ExactAlgorithm, photo.PerceptualHash, photo.PerceptualAlgorithm, photo.MediaType, photo.ByteSize, photo.Width, photo.Height, now)
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
	if _, err := tx.Exec(ctx, `UPDATE listing_signal_jobs SET status='pending',attempts=attempts+1,next_attempt_at=$1,claimed_at=NULL,last_error=$2,updated_at=$3 WHERE listing_id=$4 AND generation=$5`, now.Add(time.Duration(delay)*time.Second), message, now, job.Listing.ID, job.Generation); err != nil {
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
