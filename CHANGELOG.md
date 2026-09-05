# Changelog

All notable changes to Latvia Home Radar will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/viktorstiunovs/latvia-home-radar-bot/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/viktorstiunovs/latvia-home-radar-bot/releases/tag/v0.1.0
[0.2.0]: https://github.com/viktorstiunovs/latvia-home-radar-bot/releases/tag/v0.2.0
