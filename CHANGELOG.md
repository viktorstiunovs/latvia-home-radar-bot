# Changelog

All notable changes to Latvia Home Radar will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-09-09

### Added

- Added an administrator-only `/broadcast` command for messaging all registered
  users with isolated delivery failures and a completion summary.
- Retained each listing's price observations and added localized, photo-rich
  Telegram alerts for increases and decreases when the new price matches a
  saved filter, using green/decrease and red/increase indicators, a
  struck-through previous price, an emphasized current price, and the signed
  percentage change.
- Retained complete provider descriptions separately from Telegram titles and
  added versioned normalized address/description evidence for duplicate
  matching and future analytics.
- Added bounded, provider-host-restricted photo signal collection with SHA-256
  and perceptual fingerprints, immutable evidence snapshots, durable attempt
  outcomes, atomic completion, retries, and incremental baseline backfill
  without retaining downloaded image bodies.
- Added versioned conservative duplicate resolution in shadow mode with bounded
  candidate generation, explainable score evidence, durable accepted,
  ambiguous, and rejected decisions, stable property identities, append-only
  membership history, operator correction primitives, and inspection views.
- Added tri-state listing availability, append-only lifecycle observations,
  active-only current-offer queries, and bounded provider-specific checks that
  recognize accessible archived SS.lv adverts and City24 removal responses
  while retaining inconclusive results as unknown and preserving all history.
- Added rollout-controlled duplicate-aware saved alerts: confident duplicate
  reposts are suppressed per property and filter, true inactive relistings
  notify on higher or lower matching prices, concurrent cheaper offers get
  localized rich alerts, and normal/cheaper messages include confirmed-active
  provider alternatives. Event-time listing and alternative snapshots keep
  retries stable; ambiguous candidates remain normal listings.

### Changed

- Made the PostgreSQL data location and PostgreSQL/RabbitMQ credentials
  configurable through the Compose environment.
- Consolidated the unreleased price-history, identity, availability, and
  duplicate-notification schema into one production migration, and replaced
  four interdependent rollout flags with one `DEDUPLICATION_ENABLED` switch.
- Reused photo fingerprints for unchanged provider URLs and stopped
  price-only listing observations from regenerating identity work.
- Replaced blanket daily advert checks with feed-aware stale-listing checks,
  immediate checks required by duplicate decisions, inactive-listing dormancy,
  and capped exponential scheduling for inconclusive provider responses.
- Prevented duplicate-aware discovery events from stalling when property
  membership changes during processing: missing availability checks are now
  prioritized automatically, transient RabbitMQ deliveries use a durable
  delayed retry queue, and concurrent property-resolution commits are
  serialized to avoid PostgreSQL deadlocks.

## [0.2.0] - 2026-09-05

### Added

- English, Latvian, and Russian localization.
- Automatic Telegram language detection and `/language` selection.
- Persistent user language preferences.
- Localized filter workflows and listing notifications.

## [0.1.0] - 2026-09-04

### Added

- Initial Go release of the Telegram bot. Migrated from Python.
- SS.lv and City24 listing discovery for apartment and house sales and rentals.
- User-defined area, price, room, and size filters managed through Telegram.
- PostgreSQL persistence with embedded Goose migrations.
- RabbitMQ event delivery using a transactional outbox, publisher confirms, and
  an idempotent listing matcher.
- Telegram photo albums, reusable file IDs, structured logging, and graceful
  shutdown.

### Changed

- Replaced the Python application while retaining compatibility with its
  existing PostgreSQL data.

[Unreleased]: https://github.com/viktorstiunovs/latvia-home-radar-bot/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/viktorstiunovs/latvia-home-radar-bot/compare/v0.2.0...v1.0.0
[0.1.0]: https://github.com/viktorstiunovs/latvia-home-radar-bot/releases/tag/v0.1.0
[0.2.0]: https://github.com/viktorstiunovs/latvia-home-radar-bot/releases/tag/v0.2.0
