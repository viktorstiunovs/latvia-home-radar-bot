-- name: ListReusablePhotoFingerprints :many
SELECT DISTINCT ON (source_url)
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
WHERE listing_id = sqlc.arg(listing_id)
ORDER BY source_url, created_at DESC, id DESC;

-- name: InsertListingSignalSnapshot :one
INSERT INTO listing_signal_snapshots (
    listing_id,
    input_hash,
    normalization_version,
    description,
    normalized_address,
    normalized_description,
    structured_facts,
    photo_urls,
    completed_at
) VALUES (
    sqlc.arg(listing_id),
    sqlc.arg(input_hash),
    sqlc.arg(normalization_version),
    sqlc.arg(description),
    sqlc.arg(normalized_address),
    sqlc.arg(normalized_description),
    sqlc.arg(structured_facts),
    sqlc.arg(photo_urls),
    sqlc.arg(completed_at)
)
ON CONFLICT (listing_id, input_hash, normalization_version) DO NOTHING
RETURNING id;

-- name: InsertPhotoFingerprint :exec
INSERT INTO listing_photo_fingerprints (
    snapshot_id,
    listing_id,
    position,
    source_url,
    exact_hash,
    exact_algorithm,
    perceptual_hash,
    perceptual_algorithm,
    media_type,
    byte_size,
    width,
    height,
    created_at
) VALUES (
    sqlc.arg(snapshot_id),
    sqlc.arg(listing_id),
    sqlc.arg(position),
    sqlc.arg(source_url),
    sqlc.arg(exact_hash),
    sqlc.arg(exact_algorithm),
    sqlc.arg(perceptual_hash),
    sqlc.arg(perceptual_algorithm),
    sqlc.arg(media_type),
    sqlc.arg(byte_size),
    sqlc.arg(width),
    sqlc.arg(height),
    sqlc.arg(created_at)
)
ON CONFLICT (snapshot_id, position) DO NOTHING;

