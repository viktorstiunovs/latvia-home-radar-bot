-- +goose Up
ALTER TABLE notifications
    DROP CONSTRAINT notifications_type_check;

UPDATE notifications
SET notification_type = 'price_changed'
WHERE notification_type = 'price_decreased';

ALTER TABLE notifications
    ADD CONSTRAINT notifications_type_check
        CHECK (notification_type IN ('listing_discovered', 'price_changed'));

-- +goose Down
ALTER TABLE notifications
    DROP CONSTRAINT notifications_type_check;

DELETE FROM notifications
WHERE notification_type = 'price_changed'
  AND current_price_eur > previous_price_eur;

UPDATE notifications
SET notification_type = 'price_decreased'
WHERE notification_type = 'price_changed';

ALTER TABLE notifications
    ADD CONSTRAINT notifications_type_check
        CHECK (notification_type IN ('listing_discovered', 'price_decreased'));
