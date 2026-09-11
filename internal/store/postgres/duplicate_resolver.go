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
	queries := s.queries.WithTx(tx)
	snapshotIDs, err := queries.ClaimableDuplicateResolutionSnapshotIDs(ctx, sqlcgen.ClaimableDuplicateResolutionSnapshotIDsParams{
		StaleSeconds: int(staleResolutionClaim / time.Second),
		JobLimit:     int32(limit),
	})
	if err != nil {
		return nil, err
	}

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
	snapshotIDs, err := s.queries.FindDuplicateCandidateSnapshotIDs(ctx, sqlcgen.FindDuplicateCandidateSnapshotIDsParams{
		PropertyType:      string(evidence.Listing.PropertyType),
		DealType:          string(evidence.Listing.DealType),
		AreaKey:           evidence.Listing.AreaKey,
		NormalizedAddress: evidence.Listing.NormalizedAddress,
		Rooms:             evidence.Listing.Rooms,
		AreaM2:            evidence.Listing.AreaM2,
		Floor:             evidence.Listing.Floor,
		LandAreaM2:        evidence.Listing.LandAreaM2,
		CandidateLimit:    int32(limit),
		ListingID:         evidence.Listing.ID,
	})
	if err != nil {
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
	queries := s.queries.WithTx(tx)

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
	if err := queries.PrioritizePropertyAvailabilityChecks(ctx, sqlcgen.PrioritizePropertyAvailabilityChecksParams{
		UpdatedAt:         requiredTimestamptz(now),
		ExcludedListingID: job.Evidence.Listing.ID,
		PropertyID:        currentProperty,
	}); err != nil {
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
	if _, err := tx.Exec(ctx, `
		UPDATE duplicate_resolution_jobs
		SET status = 'pending',
		    attempts = attempts + 1,
		    next_attempt_at = $1,
		    claimed_at = NULL,
		    last_error = $2,
		    updated_at = $3
		WHERE snapshot_id = $4
	`, now.Add(time.Duration(delay)*time.Second), message, now, job.Evidence.SnapshotID); err != nil {
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

func loadListingEvidence(ctx context.Context, db sqlcgen.DBTX, snapshotID int64) (domain.ListingEvidence, error) {
	queries := sqlcgen.New(db)
	row, err := queries.LoadListingEvidence(ctx, snapshotID)
	if err != nil {
		return domain.ListingEvidence{}, err
	}
	result := domain.ListingEvidence{
		SnapshotID:  row.SnapshotID,
		CompletedAt: row.CompletedAt.Time,
		Listing: domain.Listing{
			ID:                    row.ListingID,
			Source:                row.Source,
			ExternalID:            row.ExternalID,
			URL:                   row.URL,
			Description:           row.Description,
			NormalizedAddress:     row.NormalizedAddress,
			NormalizedDescription: row.NormalizedDescription,
		},
	}
	var facts evidenceFacts
	if err := json.Unmarshal(row.StructuredFacts, &facts); err != nil {
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
	photos, err := queries.ListSnapshotPhotoFingerprints(ctx, snapshotID)
	if err != nil {
		return result, err
	}
	for _, photo := range photos {
		result.Photos = append(result.Photos, domain.PhotoFingerprint{
			Position:            int(photo.Position),
			SourceURL:           photo.SourceURL,
			ExactHash:           photo.ExactHash,
			ExactAlgorithm:      photo.ExactAlgorithm,
			PerceptualHash:      photo.PerceptualHash,
			PerceptualAlgorithm: photo.PerceptualAlgorithm,
			MediaType:           photo.MediaType,
			ByteSize:            photo.ByteSize,
			Width:               photo.Width,
			Height:              photo.Height,
		})
	}
	return result, nil
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
	row, err := sqlcgen.New(tx).InsertDuplicateDecision(ctx, sqlcgen.InsertDuplicateDecisionParams{
		LeftListingID:   left.Listing.ID,
		RightListingID:  right.Listing.ID,
		LeftSnapshotID:  left.SnapshotID,
		RightSnapshotID: right.SnapshotID,
		RuleVersion:     decision.RuleVersion,
		Confidence:      decision.Confidence,
		DecisionStatus:  string(decision.Status),
		Evidence:        evidenceJSON,
		EvaluatedAt:     requiredTimestamptz(now),
	})
	return row.ID, domain.DuplicateDecisionStatus(row.Status), err
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
