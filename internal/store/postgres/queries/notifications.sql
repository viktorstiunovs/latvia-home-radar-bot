-- name: ListPendingNotifications :many
SELECT
    notification.id AS notification_id,
    notification.filter_id,
    notification.attempts,
    notification.notification_type,
    notification.previous_price_eur,
    notification.current_price_eur,
    notification.property_id,
    notification.comparison_listing_id,
    notification.listing_snapshot,
    notification.active_alternatives,
    app_user.telegram_user_id,
    coalesce(app_user.name, '') AS user_name,
    app_user.chat_id,
    app_user.language_tag,
    listing.id AS listing_id,
    listing.source,
    listing.external_id,
    listing.url,
    listing.property_type,
    listing.deal_type,
    listing.title,
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
    coalesce(source_area.raw_name, '') AS source_area_name
FROM notifications AS notification
JOIN filters AS filter
  ON filter.id = notification.filter_id
JOIN users AS app_user
  ON app_user.id = filter.user_id
JOIN listings AS listing
  ON listing.id = notification.listing_id
LEFT JOIN areas AS area
  ON area.id = listing.area_id
LEFT JOIN areas AS parent_area
  ON parent_area.id = area.parent_id
LEFT JOIN source_areas AS source_area
  ON source_area.id = listing.source_area_id
WHERE notification.status = 'pending'
  AND notification.next_attempt_at <= now()
  AND filter.enabled = TRUE
ORDER BY notification.next_attempt_at, notification.id
LIMIT sqlc.arg(notification_limit);

-- name: InsertMatchedNotification :execrows
INSERT INTO notifications (
    filter_id,
    listing_id,
    trigger_event_id,
    notification_type,
    previous_price_eur,
    current_price_eur,
    listing_snapshot,
    status,
    next_attempt_at
) VALUES (
    sqlc.arg(filter_id),
    sqlc.arg(listing_id),
    sqlc.arg(event_id)::text::uuid,
    sqlc.arg(notification_type),
    sqlc.narg(previous_price_eur),
    sqlc.narg(current_price_eur),
    sqlc.arg(listing_snapshot),
    'pending',
    now()
)
ON CONFLICT (filter_id, trigger_event_id) DO NOTHING;

-- name: InsertDuplicateAwareNotification :execrows
INSERT INTO notifications (
    filter_id,
    listing_id,
    trigger_event_id,
    notification_type,
    previous_price_eur,
    current_price_eur,
    property_id,
    comparison_listing_id,
    listing_snapshot,
    active_alternatives,
    status,
    next_attempt_at
) VALUES (
    sqlc.arg(filter_id),
    sqlc.arg(listing_id),
    sqlc.arg(event_id)::text::uuid,
    sqlc.arg(notification_type),
    sqlc.narg(previous_price_eur),
    sqlc.narg(current_price_eur),
    sqlc.arg(property_id),
    sqlc.narg(comparison_listing_id),
    sqlc.arg(listing_snapshot),
    sqlc.arg(active_alternatives),
    'pending',
    now()
)
ON CONFLICT DO NOTHING;

