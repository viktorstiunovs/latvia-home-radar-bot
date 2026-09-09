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

const (
	staleResolutionClaim            = 10 * time.Minute
	duplicateResolutionCommitLockID = int64(0x4c48412d445550)
)

func (s *Store) ClaimDuplicateResolutionJobs(ctx context.Context, limit int) ([]domain.DuplicateResolutionJob, error) {
	if limit <= 0 {
		return nil, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT snapshot_id FROM duplicate_resolution_jobs WHERE (status='pending' AND next_attempt_at<=now()) OR (status='processing' AND claimed_at<now()-($1 * interval '1 second')) ORDER BY next_attempt_at,snapshot_id FOR UPDATE SKIP LOCKED LIMIT $2`, int(staleResolutionClaim/time.Second), limit)
	if err != nil {
		return nil, err
	}
	var snapshotIDs []int64
	for rows.Next() {
		var snapshotID int64
		if err := rows.Scan(&snapshotID); err != nil {
			rows.Close()
			return nil, err
		}
		snapshotIDs = append(snapshotIDs, snapshotID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	now := time.Now().UTC()
	result := make([]domain.DuplicateResolutionJob, 0, len(snapshotIDs))
	for _, snapshotID := range snapshotIDs {
		if _, err := tx.Exec(ctx, `UPDATE duplicate_resolution_attempts SET finished_at=$1,outcome='failed',error='worker claim expired' WHERE snapshot_id=$2 AND outcome='processing'`, now, snapshotID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE duplicate_resolution_jobs SET status='processing',claimed_at=$1,updated_at=$1 WHERE snapshot_id=$2`, now, snapshotID); err != nil {
			return nil, err
		}
		var attemptID int64
		if err := tx.QueryRow(ctx, `INSERT INTO duplicate_resolution_attempts(snapshot_id,started_at) VALUES($1,$2) RETURNING id`, snapshotID, now).Scan(&attemptID); err != nil {
			return nil, err
		}
		evidence, err := loadListingEvidence(ctx, tx, snapshotID)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.DuplicateResolutionJob{Evidence: evidence, AttemptID: attemptID})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) DuplicateCandidates(ctx context.Context, evidence domain.ListingEvidence, limit int) ([]domain.ListingEvidence, error) {
	if limit <= 0 {
		return nil, nil
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `WITH latest AS (SELECT DISTINCT ON (s.listing_id) s.id,s.listing_id,s.normalized_address,s.structured_facts,s.completed_at FROM listing_signal_snapshots s WHERE s.listing_id<>$1 ORDER BY s.listing_id,s.completed_at DESC,s.id DESC) SELECT id FROM latest WHERE structured_facts->>'property_type'=$2 AND structured_facts->>'deal_type'=$3 AND (($4<>'' AND structured_facts->>'area_key'=$4) OR ($5<>'' AND normalized_address=$5)) AND ($6::integer IS NULL OR structured_facts->>'rooms' IS NULL OR (structured_facts->>'rooms')::integer=$6) AND ($7::double precision IS NULL OR structured_facts->>'area_m2' IS NULL OR abs((structured_facts->>'area_m2')::double precision-$7)<=greatest(5.0,$7*0.08)) AND ($8::integer IS NULL OR structured_facts->>'floor' IS NULL OR (structured_facts->>'floor')::integer=$8) AND ($9::double precision IS NULL OR structured_facts->>'land_area_m2' IS NULL OR abs((structured_facts->>'land_area_m2')::double precision-$9)<=greatest(50.0,$9*0.10)) ORDER BY (normalized_address=$5 AND $5<>'') DESC,completed_at DESC,id DESC LIMIT $10`, evidence.Listing.ID, evidence.Listing.PropertyType, evidence.Listing.DealType, evidence.Listing.AreaKey, evidence.Listing.NormalizedAddress, evidence.Listing.Rooms, evidence.Listing.AreaM2, evidence.Listing.Floor, evidence.Listing.LandAreaM2, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var snapshotIDs []int64
	for rows.Next() {
		var snapshotID int64
		if err := rows.Scan(&snapshotID); err != nil {
			return nil, err
		}
		snapshotIDs = append(snapshotIDs, snapshotID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]domain.ListingEvidence, 0, len(snapshotIDs))
	for _, snapshotID := range snapshotIDs {
		candidate, err := loadListingEvidence(ctx, s.pool, snapshotID)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, nil
}

func (s *Store) CompleteDuplicateResolutionJob(ctx context.Context, job domain.DuplicateResolutionJob, decisions []domain.DuplicateDecision) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockDuplicateResolutionCommit(ctx, tx); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM duplicate_resolution_jobs WHERE snapshot_id=$1 FOR UPDATE`, job.Evidence.SnapshotID).Scan(&status); err != nil {
		return err
	}
	if status == "completed" {
		return tx.Commit(ctx)
	}
	now := time.Now().UTC()
	currentProperty, err := ensureListingProperty(ctx, tx, job.Evidence.Listing.ID, nil, "shadow_initial", now)
	if err != nil {
		return err
	}
	accepted := 0
	for _, decision := range decisions {
		decisionID, decisionStatus, err := insertDuplicateDecision(ctx, tx, job.Evidence, decision, now)
		if err != nil {
			return err
		}
		if decisionStatus != domain.DuplicateAccepted {
			continue
		}
		accepted++
		candidateProperty, err := ensureListingProperty(ctx, tx, decision.Candidate.Listing.ID, &decisionID, "shadow_candidate", now)
		if err != nil {
			return err
		}
		currentProperty, err = mergeProperties(ctx, tx, currentProperty, candidateProperty, decisionID, now)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE listing_availability_jobs SET next_attempt_at='1970-01-01 00:00:00+00',priority_requested=TRUE,updated_at=$1 WHERE status='pending' AND listing_id<>$2 AND listing_id IN (SELECT m.listing_id FROM property_listing_memberships m JOIN listings l ON l.id=m.listing_id WHERE m.property_id=$3 AND m.valid_to IS NULL AND l.availability_status<>'inactive')`, now, job.Evidence.Listing.ID, currentProperty); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE duplicate_resolution_jobs SET status='completed',attempts=attempts+1,claimed_at=NULL,last_error=NULL,updated_at=$1 WHERE snapshot_id=$2`, now, job.Evidence.SnapshotID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE duplicate_resolution_attempts SET finished_at=$1,outcome='completed',candidate_count=$2,accepted_count=$3,error=NULL WHERE id=$4 AND outcome='processing'`, now, len(decisions), accepted, job.AttemptID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RetryDuplicateResolutionJob(ctx context.Context, job domain.DuplicateResolutionJob, cause error) error {
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
	if err := tx.QueryRow(ctx, `SELECT attempts FROM duplicate_resolution_jobs WHERE snapshot_id=$1 FOR UPDATE`, job.Evidence.SnapshotID).Scan(&attempts); err != nil {
		return err
	}
	delay := 5 * (1 << min(attempts, 6))
	if delay > 300 {
		delay = 300
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE duplicate_resolution_jobs SET status='pending',attempts=attempts+1,next_attempt_at=$1,claimed_at=NULL,last_error=$2,updated_at=$3 WHERE snapshot_id=$4`, now.Add(time.Duration(delay)*time.Second), message, now, job.Evidence.SnapshotID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE duplicate_resolution_attempts SET finished_at=$1,outcome='failed',error=$2 WHERE id=$3 AND outcome='processing'`, now, message, job.AttemptID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) OverrideDuplicateDecision(ctx context.Context, decisionID int64, status domain.DuplicateDecisionStatus, reason string) error {
	if !validDuplicateStatus(status) || reason == "" {
		return fmt.Errorf("invalid duplicate decision override")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE duplicate_decisions SET status=$1,overridden_at=$2,override_reason=$3 WHERE id=$4`, status, time.Now().UTC(), reason, decisionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Store) ReassignListingProperty(ctx context.Context, listingID int64, targetPropertyID *int64, reason string) (int64, error) {
	if reason == "" {
		return 0, fmt.Errorf("property reassignment reason is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err := lockDuplicateResolutionCommit(ctx, tx); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	var currentPropertyID int64
	err = tx.QueryRow(ctx, `SELECT property_id FROM property_listing_memberships WHERE listing_id=$1 AND valid_to IS NULL FOR UPDATE`, listingID).Scan(&currentPropertyID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if targetPropertyID == nil {
		var created int64
		if err := tx.QueryRow(ctx, `INSERT INTO properties(created_at) VALUES($1) RETURNING id`, now).Scan(&created); err != nil {
			return 0, err
		}
		targetPropertyID = &created
	} else {
		var mergedInto *int64
		if err := tx.QueryRow(ctx, `SELECT merged_into_property_id FROM properties WHERE id=$1 FOR UPDATE`, *targetPropertyID).Scan(&mergedInto); err != nil {
			return 0, err
		}
		if mergedInto != nil {
			return 0, fmt.Errorf("target property %d has been merged", *targetPropertyID)
		}
	}
	if currentPropertyID == *targetPropertyID {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return *targetPropertyID, nil
	}
	if currentPropertyID != 0 {
		if _, err := tx.Exec(ctx, `UPDATE property_listing_memberships SET valid_to=$1 WHERE listing_id=$2 AND valid_to IS NULL`, now, listingID); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO property_listing_memberships(property_id,listing_id,valid_from,reason) VALUES($1,$2,$3,$4)`, *targetPropertyID, listingID, now, reason); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return *targetPropertyID, nil
}

type evidenceFacts struct {
	PropertyType   domain.PropertyType `json:"property_type"`
	DealType       domain.DealType     `json:"deal_type"`
	AreaKey        string              `json:"area_key"`
	City           string              `json:"city"`
	District       string              `json:"district"`
	Rooms          *int                `json:"rooms"`
	AreaM2         *float64            `json:"area_m2"`
	Floor          *int                `json:"floor"`
	TotalFloors    *int                `json:"total_floors"`
	BuildingSeries string              `json:"building_series"`
	BuildingType   string              `json:"building_type"`
	LandAreaM2     *float64            `json:"land_area_m2"`
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadListingEvidence(ctx context.Context, query queryRower, snapshotID int64) (domain.ListingEvidence, error) {
	var result domain.ListingEvidence
	var factsJSON []byte
	err := query.QueryRow(ctx, `SELECT s.id,s.completed_at,s.description,s.normalized_address,s.normalized_description,s.structured_facts,l.id,l.source,l.external_id,l.url FROM listing_signal_snapshots s JOIN listings l ON l.id=s.listing_id WHERE s.id=$1`, snapshotID).Scan(&result.SnapshotID, &result.CompletedAt, &result.Listing.Description, &result.Listing.NormalizedAddress, &result.Listing.NormalizedDescription, &factsJSON, &result.Listing.ID, &result.Listing.Source, &result.Listing.ExternalID, &result.Listing.URL)
	if err != nil {
		return result, err
	}
	var facts evidenceFacts
	if err := json.Unmarshal(factsJSON, &facts); err != nil {
		return result, err
	}
	result.Listing.PropertyType = facts.PropertyType
	result.Listing.DealType = facts.DealType
	result.Listing.AreaKey = facts.AreaKey
	result.Listing.City = facts.City
	result.Listing.District = facts.District
	result.Listing.Rooms = facts.Rooms
	result.Listing.AreaM2 = facts.AreaM2
	result.Listing.Floor = facts.Floor
	result.Listing.TotalFloors = facts.TotalFloors
	result.Listing.BuildingSeries = facts.BuildingSeries
	result.Listing.BuildingType = facts.BuildingType
	result.Listing.LandAreaM2 = facts.LandAreaM2
	rows, err := query.Query(ctx, `SELECT position,source_url,exact_hash,exact_algorithm,perceptual_hash,perceptual_algorithm,media_type,byte_size,width,height FROM listing_photo_fingerprints WHERE snapshot_id=$1 ORDER BY position`, snapshotID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var photo domain.PhotoFingerprint
		if err := rows.Scan(&photo.Position, &photo.SourceURL, &photo.ExactHash, &photo.ExactAlgorithm, &photo.PerceptualHash, &photo.PerceptualAlgorithm, &photo.MediaType, &photo.ByteSize, &photo.Width, &photo.Height); err != nil {
			return result, err
		}
		result.Photos = append(result.Photos, photo)
	}
	return result, rows.Err()
}

func lockDuplicateResolutionCommit(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, duplicateResolutionCommitLockID); err != nil {
		return fmt.Errorf("lock duplicate resolution commit: %w", err)
	}
	return nil
}

func insertDuplicateDecision(ctx context.Context, tx pgx.Tx, current domain.ListingEvidence, decision domain.DuplicateDecision, now time.Time) (int64, domain.DuplicateDecisionStatus, error) {
	left, right := current, decision.Candidate
	if left.Listing.ID > right.Listing.ID {
		left, right = right, left
	}
	evidenceJSON, err := json.Marshal(decision.Evidence)
	if err != nil {
		return 0, "", err
	}
	var decisionID int64
	var status string
	err = tx.QueryRow(ctx, `INSERT INTO duplicate_decisions(left_listing_id,right_listing_id,left_snapshot_id,right_snapshot_id,rule_version,confidence,automatic_status,status,evidence,evaluated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,$9) ON CONFLICT(left_snapshot_id,right_snapshot_id,rule_version) DO UPDATE SET confidence=duplicate_decisions.confidence RETURNING id,status`, left.Listing.ID, right.Listing.ID, left.SnapshotID, right.SnapshotID, decision.RuleVersion, decision.Confidence, decision.Status, evidenceJSON, now).Scan(&decisionID, &status)
	return decisionID, domain.DuplicateDecisionStatus(status), err
}

func ensureListingProperty(ctx context.Context, tx pgx.Tx, listingID int64, decisionID *int64, reason string, now time.Time) (int64, error) {
	var propertyID int64
	err := tx.QueryRow(ctx, `SELECT property_id FROM property_listing_memberships WHERE listing_id=$1 AND valid_to IS NULL FOR UPDATE`, listingID).Scan(&propertyID)
	if err == nil {
		return propertyID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO properties(created_at) VALUES($1) RETURNING id`, now).Scan(&propertyID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO property_listing_memberships(property_id,listing_id,decision_id,valid_from,reason) VALUES($1,$2,$3,$4,$5)`, propertyID, listingID, decisionID, now, reason); err != nil {
		return 0, err
	}
	return propertyID, nil
}

func mergeProperties(ctx context.Context, tx pgx.Tx, leftProperty, rightProperty, decisionID int64, now time.Time) (int64, error) {
	if leftProperty == rightProperty {
		return leftProperty, nil
	}
	survivor, merged := min(leftProperty, rightProperty), max(leftProperty, rightProperty)
	rows, err := tx.Query(ctx, `SELECT listing_id FROM property_listing_memberships WHERE property_id=$1 AND valid_to IS NULL ORDER BY listing_id FOR UPDATE`, merged)
	if err != nil {
		return 0, err
	}
	var listings []int64
	for rows.Next() {
		var listingID int64
		if err := rows.Scan(&listingID); err != nil {
			rows.Close()
			return 0, err
		}
		listings = append(listings, listingID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `UPDATE property_listing_memberships SET valid_to=$1 WHERE property_id=$2 AND valid_to IS NULL`, now, merged); err != nil {
		return 0, err
	}
	for _, listingID := range listings {
		if _, err := tx.Exec(ctx, `INSERT INTO property_listing_memberships(property_id,listing_id,decision_id,valid_from,reason) VALUES($1,$2,$3,$4,'shadow_merge')`, survivor, listingID, decisionID, now); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE properties SET merged_into_property_id=$1,merged_at=$2 WHERE id=$3 AND merged_into_property_id IS NULL`, survivor, now, merged); err != nil {
		return 0, err
	}
	return survivor, nil
}

func validDuplicateStatus(status domain.DuplicateDecisionStatus) bool {
	return status == domain.DuplicateAccepted || status == domain.DuplicateAmbiguous || status == domain.DuplicateRejected
}
