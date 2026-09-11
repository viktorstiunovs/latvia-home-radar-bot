-- name: ClaimableListingSignalJobs :many
SELECT listing_id, generation
FROM listing_signal_jobs
WHERE (
        status = 'pending'
        AND next_attempt_at <= now()
    )
   OR (
        status = 'processing'
        AND claimed_at < now() - (sqlc.arg(stale_seconds)::integer * interval '1 second')
    )
ORDER BY next_attempt_at, listing_id
FOR UPDATE SKIP LOCKED
LIMIT sqlc.arg(job_limit);

-- name: ClaimableDuplicateResolutionSnapshotIDs :many
SELECT snapshot_id
FROM duplicate_resolution_jobs
WHERE (
        status = 'pending'
        AND next_attempt_at <= now()
    )
   OR (
        status = 'processing'
        AND claimed_at < now() - (sqlc.arg(stale_seconds)::integer * interval '1 second')
    )
ORDER BY next_attempt_at, snapshot_id
FOR UPDATE SKIP LOCKED
LIMIT sqlc.arg(job_limit);

-- name: ClaimableAvailabilityListingIDs :many
SELECT job.listing_id
FROM listing_availability_jobs AS job
JOIN listings AS listing
  ON listing.id = job.listing_id
WHERE job.status = 'pending'
  AND job.next_attempt_at <= now()
  AND listing.availability_status <> 'inactive'
  AND (
      job.priority_requested
      OR listing.availability_status = 'unknown'
      OR listing.last_seen_at <= now() - (sqlc.arg(stale_seconds)::integer * interval '1 second')
  )
ORDER BY
    job.priority_requested DESC,
    job.next_attempt_at,
    job.listing_id
FOR UPDATE OF job SKIP LOCKED
LIMIT sqlc.arg(job_limit);

-- name: ExpireStaleAvailabilityAttempts :exec
UPDATE listing_availability_attempts AS attempt
SET finished_at = sqlc.arg(expired_at),
    outcome = 'failed',
    error = 'worker claim expired'
FROM listing_availability_jobs AS job
WHERE attempt.listing_id = job.listing_id
  AND attempt.outcome = 'processing'
  AND job.status = 'processing'
  AND job.claimed_at < sqlc.arg(expired_at)::timestamptz
      - (sqlc.arg(stale_seconds)::integer * interval '1 second');

-- name: ReleaseStaleAvailabilityJobs :exec
UPDATE listing_availability_jobs
SET status = 'pending',
    claimed_at = NULL,
    updated_at = sqlc.arg(released_at)
WHERE status = 'processing'
  AND claimed_at < sqlc.arg(released_at)::timestamptz
      - (sqlc.arg(stale_seconds)::integer * interval '1 second');
