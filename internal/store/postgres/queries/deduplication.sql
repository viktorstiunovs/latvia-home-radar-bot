-- name: FindDuplicateCandidateSnapshotIDs :many
WITH latest_snapshots AS (
    SELECT DISTINCT ON (snapshot.listing_id)
        snapshot.id,
        snapshot.listing_id,
        snapshot.normalized_address,
        snapshot.structured_facts,
        snapshot.completed_at
    FROM listing_signal_snapshots AS snapshot
    WHERE snapshot.listing_id <> sqlc.arg(listing_id)
    ORDER BY
        snapshot.listing_id,
        snapshot.completed_at DESC,
        snapshot.id DESC
)
SELECT id
FROM latest_snapshots
WHERE structured_facts->>'property_type' = sqlc.arg(property_type)::text
  AND structured_facts->>'deal_type' = sqlc.arg(deal_type)::text
  AND (
      (
          sqlc.arg(area_key)::text <> ''
          AND structured_facts->>'area_key' = sqlc.arg(area_key)
      )
      OR (
          sqlc.arg(normalized_address)::text <> ''
          AND normalized_address = sqlc.arg(normalized_address)
      )
  )
  AND (
      sqlc.narg(rooms)::integer IS NULL
      OR structured_facts->>'rooms' IS NULL
      OR (structured_facts->>'rooms')::integer = sqlc.narg(rooms)
  )
  AND (
      sqlc.narg(area_m2)::double precision IS NULL
      OR structured_facts->>'area_m2' IS NULL
      OR abs(
          (structured_facts->>'area_m2')::double precision
          - sqlc.narg(area_m2)
      ) <= greatest(5.0, sqlc.narg(area_m2) * 0.08)
  )
  AND (
      sqlc.narg(floor)::integer IS NULL
      OR structured_facts->>'floor' IS NULL
      OR (structured_facts->>'floor')::integer = sqlc.narg(floor)
  )
  AND (
      sqlc.narg(land_area_m2)::double precision IS NULL
      OR structured_facts->>'land_area_m2' IS NULL
      OR abs(
          (structured_facts->>'land_area_m2')::double precision
          - sqlc.narg(land_area_m2)
      ) <= greatest(50.0, sqlc.narg(land_area_m2) * 0.10)
  )
ORDER BY
    (
        normalized_address = sqlc.arg(normalized_address)
        AND sqlc.arg(normalized_address)::text <> ''
    ) DESC,
    completed_at DESC,
    id DESC
LIMIT sqlc.arg(candidate_limit);

-- name: LoadListingEvidence :one
SELECT
    snapshot.id AS snapshot_id,
    snapshot.completed_at,
    snapshot.description,
    snapshot.normalized_address,
    snapshot.normalized_description,
    snapshot.structured_facts,
    listing.id AS listing_id,
    listing.source,
    listing.external_id,
    listing.url
FROM listing_signal_snapshots AS snapshot
JOIN listings AS listing
  ON listing.id = snapshot.listing_id
WHERE snapshot.id = sqlc.arg(snapshot_id);

-- name: ListSnapshotPhotoFingerprints :many
SELECT
    position,
    source_url,
    exact_hash,
    exact_algorithm,
    perceptual_hash,
    perceptual_algorithm,
    media_type,
    byte_size,
    width,
    height
FROM listing_photo_fingerprints
WHERE snapshot_id = sqlc.arg(snapshot_id)
ORDER BY position;

-- name: InsertDuplicateDecision :one
INSERT INTO duplicate_decisions (
    left_listing_id,
    right_listing_id,
    left_snapshot_id,
    right_snapshot_id,
    rule_version,
    confidence,
    automatic_status,
    status,
    evidence,
    evaluated_at
) VALUES (
    sqlc.arg(left_listing_id),
    sqlc.arg(right_listing_id),
    sqlc.arg(left_snapshot_id),
    sqlc.arg(right_snapshot_id),
    sqlc.arg(rule_version),
    sqlc.arg(confidence),
    sqlc.arg(decision_status),
    sqlc.arg(decision_status),
    sqlc.arg(evidence),
    sqlc.arg(evaluated_at)
)
ON CONFLICT (left_snapshot_id, right_snapshot_id, rule_version) DO UPDATE
SET confidence = duplicate_decisions.confidence
RETURNING id, status;

-- name: ResolvedPropertyIDForListing :one
SELECT membership.property_id
FROM property_listing_memberships AS membership
WHERE membership.listing_id = sqlc.arg(listing_id)
  AND membership.valid_to IS NULL
  AND EXISTS (
      SELECT 1
      FROM listing_signal_snapshots AS snapshot
      JOIN duplicate_resolution_jobs AS resolution_job
        ON resolution_job.snapshot_id = snapshot.id
       AND resolution_job.status = 'completed'
      WHERE snapshot.listing_id = sqlc.arg(listing_id)
  );

-- name: UserWasNotifiedAboutProperty :one
SELECT EXISTS (
    SELECT 1
    FROM notifications AS notification
    JOIN filters AS filter
      ON filter.id = notification.filter_id
    JOIN property_listing_memberships AS membership
      ON membership.listing_id = notification.listing_id
     AND membership.valid_to IS NULL
    WHERE filter.user_id = sqlc.arg(user_id)
      AND membership.property_id = sqlc.arg(property_id)
);

-- name: PrioritizePropertyAvailabilityChecks :exec
UPDATE listing_availability_jobs
SET next_attempt_at = '1970-01-01 00:00:00+00',
    priority_requested = TRUE,
    updated_at = sqlc.arg(updated_at)
WHERE listing_availability_jobs.status = 'pending'
  AND listing_availability_jobs.listing_id <> sqlc.arg(excluded_listing_id)
  AND listing_availability_jobs.listing_id IN (
      SELECT membership.listing_id
      FROM property_listing_memberships AS membership
      JOIN listings AS listing
        ON listing.id = membership.listing_id
      WHERE membership.property_id = sqlc.arg(property_id)
        AND membership.valid_to IS NULL
        AND listing.availability_status <> 'inactive'
  );

-- name: ScheduleMissingPropertyAvailabilityChecks :one
WITH missing_listings AS MATERIALIZED (
    SELECT membership.listing_id
    FROM property_listing_memberships AS membership
    JOIN listings AS listing
      ON listing.id = membership.listing_id
    WHERE membership.property_id = sqlc.arg(property_id)
      AND membership.valid_to IS NULL
      AND membership.listing_id <> sqlc.arg(excluded_listing_id)
      AND listing.availability_status <> 'inactive'
      AND NOT EXISTS (
          SELECT 1
          FROM listing_availability_observations AS observation
          WHERE observation.listing_id = membership.listing_id
            AND observation.observation_kind = 'provider_check'
            AND observation.observed_at >= sqlc.arg(observed_at)
      )
),
scheduled AS (
    INSERT INTO listing_availability_jobs (
        listing_id,
        next_attempt_at,
        priority_requested,
        created_at,
        updated_at
    )
    SELECT
        listing_id,
        now(),
        TRUE,
        now(),
        now()
    FROM missing_listings
    ON CONFLICT (listing_id) DO UPDATE
    SET next_attempt_at = CASE
            WHEN listing_availability_jobs.status = 'pending'
                THEN least(listing_availability_jobs.next_attempt_at, excluded.next_attempt_at)
            ELSE listing_availability_jobs.next_attempt_at
        END,
        priority_requested = CASE
            WHEN listing_availability_jobs.status = 'pending' THEN TRUE
            ELSE listing_availability_jobs.priority_requested
        END,
        updated_at = CASE
            WHEN listing_availability_jobs.status = 'pending' THEN excluded.updated_at
            ELSE listing_availability_jobs.updated_at
        END
    RETURNING listing_id
)
SELECT count(*)
FROM missing_listings;

-- name: ListOtherPropertyOffers :many
SELECT
    listing.id,
    listing.source,
    listing.url,
    event_price.price_eur AS event_price_eur,
    last_known_price.price_eur AS last_known_price_eur,
    listing.availability_status,
    listing.first_seen_at
FROM property_listing_memberships AS membership
JOIN listings AS listing
  ON listing.id = membership.listing_id
LEFT JOIN LATERAL (
    SELECT history.price_eur
    FROM listing_price_history AS history
    WHERE history.listing_id = listing.id
      AND history.observed_at <= sqlc.arg(occurred_at)
    ORDER BY history.observed_at DESC, history.id DESC
    LIMIT 1
) AS event_price ON TRUE
LEFT JOIN LATERAL (
    SELECT history.price_eur
    FROM listing_price_history AS history
    WHERE history.listing_id = listing.id
      AND history.observed_at <= sqlc.arg(occurred_at)
      AND history.price_eur IS NOT NULL
    ORDER BY history.observed_at DESC, history.id DESC
    LIMIT 1
) AS last_known_price ON TRUE
WHERE membership.property_id = sqlc.arg(property_id)
  AND membership.valid_to IS NULL
  AND listing.id <> sqlc.arg(excluded_listing_id)
ORDER BY listing.first_seen_at DESC, listing.id DESC;
