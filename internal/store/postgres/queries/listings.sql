-- name: LoadListing :one
SELECT
    listing.id,
    listing.source,
    listing.external_id,
    listing.url,
    listing.property_type,
    listing.deal_type,
    listing.title,
    listing.description,
    listing.normalized_address,
    listing.normalized_description,
    listing.price_eur,
    listing.rooms,
    listing.area_m2,
    coalesce(listing.city, '') AS city,
    coalesce(listing.district, '') AS district,
    coalesce(listing.address, '') AS address,
    listing.floor,
    listing.total_floors,
    coalesce(listing.building_series, '') AS building_series,
    coalesce(listing.building_type, '') AS building_type,
    listing.land_area_m2,
    listing.details_enriched,
    listing.published_at,
    coalesce(listing.image_url, '') AS image_url,
    listing.photo_urls,
    coalesce(area.key, '') AS area_key,
    coalesce(area.name, '') AS area_name,
    coalesce(area.type, '') AS area_type,
    coalesce(parent_area.key, '') AS area_parent_key,
    coalesce(parent_area.name, '') AS area_parent_name,
    coalesce(source_area.external_key, '') AS source_area_key,
    coalesce(source_area.raw_name, '') AS source_area_name,
    listing.availability_status,
    listing.first_seen_at,
    listing.last_seen_at,
    listing.availability_changed_at
FROM listings AS listing
LEFT JOIN areas AS area
  ON area.id = listing.area_id
LEFT JOIN areas AS parent_area
  ON parent_area.id = area.parent_id
LEFT JOIN source_areas AS source_area
  ON source_area.id = listing.source_area_id
WHERE listing.id = sqlc.arg(listing_id);

-- name: ListStoredListingsForEnrichment :many
SELECT
    listing.external_id,
    listing.price_eur,
    coalesce(listing.description, '') AS description,
    coalesce(listing.address, '') AS address,
    listing.rooms,
    listing.area_m2,
    listing.floor,
    listing.total_floors,
    coalesce(listing.building_series, '') AS building_series,
    coalesce(listing.building_type, '') AS building_type,
    listing.land_area_m2,
    coalesce(listing.image_url, '') AS image_url,
    listing.photo_urls,
    coalesce(area.key, '') AS area_key,
    listing.details_enriched
FROM listings AS listing
LEFT JOIN areas AS area
  ON area.id = listing.area_id
WHERE listing.source = sqlc.arg(source)
  AND listing.external_id = ANY(sqlc.arg(external_ids)::text[]);

-- name: InsertListing :one
INSERT INTO listings (
    source,
    external_id,
    url,
    property_type,
    deal_type,
    price_eur,
    rooms,
    area_m2,
    city,
    district,
    address,
    floor,
    total_floors,
    building_series,
    building_type,
    land_area_m2,
    details_enriched,
    title,
    description,
    published_at,
    image_url,
    photo_urls,
    area_id,
    source_area_id,
    first_seen_at,
    availability_status,
    last_seen_at,
    availability_changed_at,
    raw_json
) VALUES (
    sqlc.arg(source),
    sqlc.arg(external_id),
    sqlc.arg(url),
    sqlc.arg(property_type),
    sqlc.arg(deal_type),
    sqlc.narg(price_eur),
    sqlc.narg(rooms),
    sqlc.narg(area_m2),
    nullif(sqlc.arg(city)::text, ''),
    nullif(sqlc.arg(district)::text, ''),
    nullif(sqlc.arg(address)::text, ''),
    sqlc.narg(floor),
    sqlc.narg(total_floors),
    nullif(sqlc.arg(building_series)::text, ''),
    nullif(sqlc.arg(building_type)::text, ''),
    sqlc.narg(land_area_m2),
    sqlc.arg(details_enriched),
    sqlc.arg(title),
    sqlc.arg(description),
    sqlc.narg(published_at),
    nullif(sqlc.arg(image_url)::text, ''),
    sqlc.arg(photo_urls),
    sqlc.narg(area_id),
    sqlc.narg(source_area_id),
    sqlc.arg(first_seen_at),
    'active',
    sqlc.arg(first_seen_at),
    sqlc.arg(first_seen_at),
    sqlc.arg(raw_json)
)
ON CONFLICT (source, external_id) DO NOTHING
RETURNING id;

-- name: UpdateExistingListing :exec
UPDATE listings
SET url = sqlc.arg(url),
    title = coalesce(nullif(sqlc.arg(title)::text, ''), title),
    description = coalesce(nullif(sqlc.arg(description)::text, ''), description),
    published_at = coalesce(sqlc.narg(published_at), published_at),
    image_url = coalesce(nullif(sqlc.arg(image_url)::text, ''), image_url),
    area_id = coalesce(sqlc.narg(area_id), area_id),
    source_area_id = coalesce(sqlc.narg(source_area_id), source_area_id),
    price_eur = sqlc.narg(price_eur),
    rooms = coalesce(sqlc.narg(rooms), rooms),
    area_m2 = coalesce(sqlc.narg(area_m2), area_m2),
    city = coalesce(nullif(sqlc.arg(city)::text, ''), city),
    district = coalesce(nullif(sqlc.arg(district)::text, ''), district),
    address = coalesce(nullif(sqlc.arg(address)::text, ''), address),
    floor = coalesce(sqlc.narg(floor), floor),
    total_floors = coalesce(sqlc.narg(total_floors), total_floors),
    building_series = coalesce(nullif(sqlc.arg(building_series)::text, ''), building_series),
    building_type = coalesce(nullif(sqlc.arg(building_type)::text, ''), building_type),
    land_area_m2 = coalesce(sqlc.narg(land_area_m2), land_area_m2),
    photo_urls = CASE
        WHEN jsonb_array_length(sqlc.arg(photo_urls)::jsonb) > 0
            THEN sqlc.arg(photo_urls)::jsonb
        ELSE photo_urls
    END,
    details_enriched = details_enriched OR sqlc.arg(details_enriched),
    raw_json = sqlc.arg(raw_json),
    availability_status = 'active',
    last_seen_at = sqlc.arg(observed_at),
    availability_changed_at = CASE
        WHEN availability_status <> 'active' THEN sqlc.arg(observed_at)
        ELSE availability_changed_at
    END
WHERE id = sqlc.arg(listing_id);

-- name: UpdateListingSignals :exec
UPDATE listings
SET description = sqlc.arg(description),
    normalized_address = sqlc.arg(normalized_address),
    normalized_description = sqlc.arg(normalized_description),
    address = coalesce(nullif(sqlc.arg(address)::text, ''), address),
    rooms = coalesce(sqlc.narg(rooms), rooms),
    area_m2 = coalesce(sqlc.narg(area_m2), area_m2),
    floor = coalesce(sqlc.narg(floor), floor),
    total_floors = coalesce(sqlc.narg(total_floors), total_floors),
    building_series = coalesce(nullif(sqlc.arg(building_series)::text, ''), building_series),
    building_type = coalesce(nullif(sqlc.arg(building_type)::text, ''), building_type),
    land_area_m2 = coalesce(sqlc.narg(land_area_m2), land_area_m2),
    image_url = coalesce(nullif(sqlc.arg(image_url)::text, ''), image_url),
    photo_urls = CASE
        WHEN jsonb_array_length(sqlc.arg(photo_urls)::jsonb) > 0
            THEN sqlc.arg(photo_urls)::jsonb
        ELSE photo_urls
    END,
    details_enriched = details_enriched OR sqlc.arg(details_enriched)
WHERE id = sqlc.arg(listing_id);
