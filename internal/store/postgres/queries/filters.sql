-- name: ListSavedFilters :many
SELECT
    filter.id,
    filter.deal_type,
    filter.property_types,
    filter.price_min,
    filter.price_max,
    filter.rooms_min,
    filter.rooms_max,
    filter.area_min,
    filter.area_max,
    filter.enabled,
    coalesce(
        array_agg(
            CASE
                WHEN parent_area.name IS NULL THEN area.name
                ELSE area.name || ' (' || parent_area.name || ')'
            END
            ORDER BY area.key
        ) FILTER (WHERE area.id IS NOT NULL),
        '{}'
    )::text[] AS area_labels
FROM filters AS filter
JOIN users AS app_user
  ON app_user.id = filter.user_id
LEFT JOIN filter_areas AS filter_area
  ON filter_area.filter_id = filter.id
LEFT JOIN areas AS area
  ON area.id = filter_area.area_id
LEFT JOIN areas AS parent_area
  ON parent_area.id = area.parent_id
WHERE app_user.telegram_user_id = sqlc.arg(telegram_user_id)
GROUP BY filter.id
ORDER BY filter.id;

-- name: ListEnabledFilters :many
SELECT
    filter.id,
    filter.user_id,
    filter.deal_type,
    filter.property_types,
    filter.price_min,
    filter.price_max,
    filter.rooms_min,
    filter.rooms_max,
    filter.area_min,
    filter.area_max,
    filter.activated_at,
    coalesce(
        array_agg(area.key ORDER BY area.key)
            FILTER (WHERE area.id IS NOT NULL),
        '{}'
    )::text[] AS area_keys
FROM filters AS filter
LEFT JOIN filter_areas AS filter_area
  ON filter_area.filter_id = filter.id
LEFT JOIN areas AS area
  ON area.id = filter_area.area_id
WHERE filter.enabled = TRUE
GROUP BY filter.id;

