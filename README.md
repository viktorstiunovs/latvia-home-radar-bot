# Latvia Home Radar

A self-hosted Go Telegram bot that watches SS.lv and City24.lv for new Latvian apartment and house listings and sends matches for user-defined filters.

## Features

- Apartments and houses for rent or sale across Latvia.
- SS.lv RSS/detail parsing and City24 JSON API discovery/enrichment.
- Price, rooms, size, and hierarchical canonical-area filters.
- First-poll baselines so deployment does not flood users with old listings.
- Telegram media albums of up to ten photos and reusable Telegram file IDs.
- PostgreSQL persistence, RabbitMQ events, embedded Goose migrations, structured audit logs, and graceful shutdown.
- One application process containing independently structured Telegram, source-monitoring, event-matching, and notification-delivery loops.

## Local setup

Go 1.26 or newer, PostgreSQL 17, and RabbitMQ 4 are recommended.

```bash
cp .env.example .env
docker compose up -d db rabbitmq
go run ./cmd/bot
```

Set `TELEGRAM_BOT_TOKEN` to a token from BotFather and `SCRAPER_CONTACT` to an email address or URL identifying the operator. The HTTP user agent includes this contact value.

Run unit tests:

```bash
go test ./...
```

Run the complete stack:

```bash
docker compose up -d --build
docker compose logs -f app
```

## Bot commands

```text
/start
/new
/alerts
/pause ID
/resume ID
/delete ID
/cancel
/help
```

The six-step inline wizard selects property type, deal type, canonical areas, price, rooms, and size. Pausing deletes pending messages for that alert; restarting updates its activation time so paused listings are not replayed. Deletion requires confirmation from the alerts screen.

## Database migration and cutover

Migrations are embedded into the binary and run at startup. The baseline migration uses idempotent creation and `ADD COLUMN IF NOT EXISTS`, so it can safely adopt the schema produced by the Python application's migrations as well as initialize a fresh database.

Before cutover:

1. Back up PostgreSQL with `pg_dump`.
2. Stop the Python container; never run both applications against one bot token.
3. Start the Go image against a restored copy first and test `/start`, alert management, one source poll, and one delivery.
4. Start the Go application against production and retain the Python revision for rollback until several polling cycles succeed.

Active filters, source baselines, listings, pending notifications, and cached Telegram file IDs use the existing tables and are retained.

## Architecture

`cmd/bot` is the composition root. Pure types and matching rules live in `internal/domain`; versioned event contracts in `internal/events`; source adapters in `internal/provider`; application orchestration in `internal/app`; PostgreSQL in `internal/store/postgres`; RabbitMQ in `internal/broker/rabbitmq`; and all Telegram-specific behavior in `internal/telegram`.

The providers normalize API/RSS/detail payloads before crossing their package boundary. Discovery stores each listing together with a `listing.discovered.v1` outbox event in one transaction. The outbox relay publishes it to RabbitMQ, and the idempotent alert matcher consumes it and creates notification rows. The existing notifier then delivers those rows through Telegram. Notifications only use persisted listing details and media URLs—they never reopen a provider detail page.
