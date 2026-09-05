-- +goose Up
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS language_tag TEXT NOT NULL DEFAULT 'en';

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_language_tag_check;

ALTER TABLE users
    ADD CONSTRAINT users_language_tag_check
    CHECK (language_tag IN ('en', 'lv', 'ru'));

-- +goose Down
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_language_tag_check;

ALTER TABLE users
    DROP COLUMN IF EXISTS language_tag;
