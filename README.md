# Latvia Home Radar

A self-hosted Go Telegram bot that watches SS.lv and City24.lv for new Latvian apartment and house listings and sends matches for user-defined filters.

## Features

- Apartments and houses for rent or sale across Latvia.
- SS.lv RSS/detail parsing and City24 JSON API discovery/enrichment.
- Price, rooms, size, and hierarchical canonical-area filters.
- First-poll baselines so deployment does not flood users with old listings.
- Telegram media albums of up to ten photos and reusable Telegram file IDs.
- English, Latvian, and Russian bot interfaces with a persisted language choice.
- PostgreSQL persistence, RabbitMQ events, embedded Goose migrations, structured audit logs, and graceful shutdown.
- One application process containing independently structured Telegram, source-monitoring, event-matching, and notification-delivery loops.

## Local setup

Go 1.26 or newer, PostgreSQL 17, and RabbitMQ 4 are recommended.

```bash
cp .env.example .env
docker compose up -d db rabbitmq
go run ./cmd/bot
```

Set `TELEGRAM_BOT_TOKEN` to a token from BotFather and `SCRAPER_CONTACT` to an email address or URL identifying the operator. The HTTP user agent includes this contact value. Replace both example infrastructure passwords before deploying the complete stack. Use URL-safe passwords because Compose also uses them in the application connection URLs; a command such as `openssl rand -hex 32` generates a suitable value.

`POSTGRES_DATA_SOURCE` accepts either a Docker named volume such as
`postgres_data` or an absolute host directory. For example, the production
server can keep PostgreSQL outside the project checkout with:

```dotenv
POSTGRES_DATA_SOURCE=/home/clive00lewis/db/postgres
```

The host directory must exist and remain owned by the PostgreSQL user in the
container. For `postgres:17-alpine`, an existing data directory normally shows
numeric owner and group `70:70`; do not change it to the host login user.

`POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `RABBITMQ_USER`, and
`RABBITMQ_PASSWORD` configure the infrastructure containers. The complete
Compose stack constructs its internal application URLs using the `db` and
`rabbitmq` service names. `DATABASE_URL` and `RABBITMQ_URL` in `.env` use
`localhost` for the documented workflow where the Go application runs directly
on the host.

PostgreSQL and RabbitMQ only apply their initialization credentials when their
data directories are first created. When adopting existing data, initially set
the variables to the credentials already stored by each service. Changing the
variables alone does not rotate credentials in an existing database or broker.

Validate the environment without printing its rendered secrets:

```bash
docker compose config --quiet
```

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
/language
/cancel
/help
```

The six-step inline wizard selects property type, deal type, canonical areas, price, rooms, and size. Pausing deletes pending messages for that alert; restarting updates its activation time so paused listings are not replayed. Deletion requires confirmation from the alerts screen.

## Localization

User-facing Telegram text lives in the embedded TOML catalogs under
`internal/localization/messages`. English is the fallback language; Latvian and
Russian are selected automatically from Telegram's language tag for new users,
and `/language` lets a user persist a different preference. Command names,
callback data, event names, provider identifiers, and database enum values are
language-independent and must not be translated.

To add or change a message:

1. Add or update its semantic message ID in `internal/localization/catalog.go`.
2. Add the same message section to all three `active.<language>.toml` catalogs.
3. Use Go template fields such as `{{.ID}}` for dynamic values and the
   appropriate CLDR plural sections (`one`, `few`, `many`, `zero`, `other`).
4. Preserve supported Telegram HTML markup and ensure provider- or user-owned
   values are HTML-escaped in Go before passing them to a message template.
5. Run `go test ./...`; catalog completeness and representative plural forms
   are checked by the localization tests.

To add another supported language, add its complete catalog, load it and add its
base IETF language tag in `internal/localization/catalog.go`, expose it in the
language keyboard, and add a new sequential migration that extends the
`users_language_tag_check` constraint. Never rewrite the existing language
migration after it may have been applied.

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
