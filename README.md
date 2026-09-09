# Latvia Home Radar

A self-hosted Go Telegram bot that watches SS.lv and City24.lv for new Latvian apartment and house listings and sends matches for user-defined filters.

## Features

- Apartments and houses for rent or sale across Latvia.
- SS.lv RSS/detail parsing and City24 JSON API discovery/enrichment.
- Price, rooms, size, and hierarchical canonical-area filters.
- Durable listing price history and rich alerts when a property's changed price
  matches a saved filter.
- Durable provider descriptions, normalized duplicate-matching signals, and
  exact/perceptual photo fingerprints prepared for conservative identity
  resolution and future analytics.
- Tri-state provider-advert availability with retained lifecycle observations,
  periodic checks, and active-only current-offer queries.
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

Set `TELEGRAM_BOT_TOKEN` to a token from BotFather and `SCRAPER_CONTACT` to an email address or URL identifying the operator. The HTTP user agent includes this contact value. Set `TELEGRAM_ADMIN_USER_ID` to your numeric Telegram user ID to enable the administrator-only `/broadcast` command; leaving it empty disables that command. Replace both example infrastructure passwords before deploying the complete stack. Use URL-safe passwords because Compose also uses them in the application connection URLs; a command such as `openssl rand -hex 32` generates a suitable value.

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

The configured administrator can send a plain-text message to every chat in the
`users` table with `/broadcast <message>`. Delivery continues when an individual
chat rejects the message, and the administrator receives successful and failed
delivery counts when the broadcast finishes. The command is intentionally
omitted from the public command list and ignored when invoked by any other user.

## Price history and alerts

Every listing's initial price observation is stored, including listings from a
source's silent first-poll baseline. Later observations are appended only when
the value changes, and the listing row always reflects the latest observation.
Known-to-unknown and unknown-to-known transitions are retained as history but
do not send alerts because a meaningful percentage cannot be calculated.

Every known-to-known increase or decrease is matched against each active saved
filter using the new price. A listing can therefore alert a user when a price
change first moves it into their configured range; filters that do not match the
new price remain silent. The notification uses a green indicator for a decrease
or a red indicator for an increase, strikes through the previous price,
emphasizes the current price, and shows the signed percentage change. It retains
those triggering values even if a newer change arrives before a retry. It uses
the persisted listing details and photos, including reusable Telegram file IDs;
delivery never fetches the provider page again.

## Duplicate-matching signals and retention

Each provider advert remains an independent durable listing record. Detail
enrichment retains the complete useful provider description separately from
the concise Telegram title. Versioned normalization derives stable address and
description values across capitalization, markup, punctuation, whitespace,
diacritics, contact tokens, and common Latvian, English, and Russian provider
boilerplate. The original description remains available for audit and future
statistics; normalized text is matching evidence rather than a replacement.

A dedicated signal collector processes new listings and incrementally
backfills existing baseline listings outside the source-polling loop. It
downloads at most eight unique photos per evidence snapshot, accepts only the
documented SS.lv or City24 image hosts (including redirects), enforces a
15-second request timeout and 10 MiB response limit, and records both a SHA-256
content fingerprint and a versioned 64-bit difference hash. Resized or
recompressed copies can therefore be compared without retaining downloaded
image bodies. Existing adverts are requeued only when an address, description,
structured property fact, or effective photo URL changes; a price-only feed
update does not regenerate identity evidence. When a new snapshot is needed,
algorithm-compatible fingerprints for unchanged photo URLs are copied forward
and only new or changed URLs are downloaded.

Evidence snapshots and collection attempts are durable, independently
queryable records. A snapshot contains the raw and normalized text, structured
property facts, source photo URLs, algorithm versions, and completed photo
fingerprints. Failures retain an observable attempt and use bounded exponential
retry; a snapshot and all of its photo fingerprints commit atomically, so later
identity resolution cannot consume a partial result. Reprocessing identical
evidence is idempotent, while materially changed evidence can produce another
immutable snapshot for longitudinal analysis.

After each evidence snapshot, a separate resolver evaluates a bounded set of
compatible candidates in shadow mode. Candidate generation first requires the
same property/deal type and a shared canonical area or exact normalized
address, then excludes known-incompatible rooms, size, floor, and land area.
The versioned `property-v1` rule combines exact or perceptual photo evidence,
address, area, structured facts, and description-token similarity. Automatic
merges require photo evidence, compatible location or structural context, no
conflicting fact, and a score of at least `0.72`; scores from `0.35` are retained
as ambiguous for review. Address or prose alone can never cause an automatic
merge.

Provider listings are linked to stable property records through append-only
membership history. Accepted decisions merge property groups without deleting
either listing or its evidence. Operators can override a retained decision and
reassign or split a listing while preserving the automatic result and previous
memberships for audit. `duplicate_decision_inspection` exposes individual
decisions and current properties; `duplicate_score_distribution` summarizes
scores by rule version and outcome. Shadow resolution does not change or
suppress notifications.

The complete deduplication pipeline is enabled by default. Set
`DEDUPLICATION_ENABLED=false` to stop signal collection, property resolution,
availability checks, and duplicate-aware delivery together. Listings, price
history, and queued work remain stored, while ordinary per-advert discovery and
price-change alerts continue. Built-in limits use two workers for each stage,
at most eight photos per snapshot, fifty database candidates per resolution,
and a 24-hour availability staleness threshold.

## Listing availability and lifecycle retention

Availability belongs to each provider advert, independently of the stable
property it may represent. `active` means the provider supplied affirmative
evidence that the advert is currently actionable, `inactive` means the provider
supplied expiry/removal evidence, and `unknown` means the check was
inconclusive. Unknown is intentionally not treated as active. `first_seen_at`
records discovery, `last_seen_at` advances only on active evidence, and
`availability_changed_at` records the latest state transition.

Every listing seen in a successful RSS/API result is recorded as active,
including the silent first-poll baseline. A listing missing from the bounded
recent-results window is not changed: absence is never interpreted as removal.
Repeated active feed sightings refresh `last_seen_at` without appending a
redundant lifecycle row. A separate bounded worker checks direct provider state
only after an active advert has received no affirmative sighting for the
configured staleness window, or when a duplicate-aware notification needs a
fresh decision. City24 status `2` is published, while HTTP `404`/`410` is
inactive. On SS.lv, a full advert with its live contact row is active; an
otherwise complete archived advert whose direct URL still works but whose
contact row has been removed is inactive. Unexpected content,
anti-bot/challenge pages, and unfamiliar City24 statuses become unknown rather
than false inactive results.

All observations and transitions are appended to
`listing_availability_observations`; checks and failures are also retained in
`listing_availability_attempts`. Changing availability never deletes provider
details, descriptions, fingerprints, property memberships, or price history.
`current_active_listing_offers` contains only confirmed active adverts, while
the base listing and lifecycle tables retain inactive and unknown history for
relisting comparisons and analytics.

The 24-hour default is both the minimum staleness before checking a
confirmed-active advert and the base delay for inconclusive results. Repeated
unknown results use a capped exponential delay. A direct check that confirms an
older advert is still active is not repeated sooner than seven days unless a
duplicate decision explicitly needs fresh evidence. Confirmed-inactive adverts
are not periodically checked; a later feed sighting reactivates them. Existing
listings are backfilled once as unknown and queued incrementally. Checks are
restricted to provider hosts, limited to 2 MiB and 15 seconds, independently
retryable, and do not block source polling.

## Duplicate-aware saved alerts

Duplicate-aware delivery is enabled by default after evidence collection,
property resolution, and availability tracking. The existing
`listing.discovered.v1` contract is unchanged. If its property decision is not
ready—or an apparently active alternative needs a fresh direct check—the event
is left unconsumed and retried. This prevents a fast RabbitMQ delivery from
racing ahead of the background evidence workers.

Each saved filter is evaluated against the new advert's initial event-time
price and persisted listing snapshot. When several filters owned by one user
match the same event, they are coalesced into one alert; different users remain
independent. A confident match is then handled as one of four cases:

- The user has not seen the property: send the normal full listing, with
  links and prices for other confirmed-active offers.
- The previous adverts are confirmed inactive and the last known price differs:
  send the approved full price-change design for a relisting, in either
  direction, if the new price matches the filter.
- Another advert is confirmed active and the new one is cheaper than the
  current best price: send one localized green cheaper-offer alert, with the
  prior/current price, signed percentage, and active alternatives.
- The property was already notified and the new concurrent offer is equal or
  more expensive: retain the advert and evidence but do not describe it as a
  property price change.

Ambiguous or rejected identity candidates have separate property IDs and
continue through the normal listing path. A price change on an existing
provider advert still produces its own increase/decrease alert in both
directions when its new price matches the filter; its price history is never
combined with another advert's history. Inactive offers remain available for
relisting comparisons but are never shown as current alternatives, and unknown
availability is not presented as active.

Notification rows retain the exact advert snapshot, triggering prices,
availability state, comparison listing, and alternative links used when they
were queued, so retries do not drift to later prices. Creation remains
idempotent by integration event, with matching filters coalesced per user.
`DEDUPLICATION_ENABLED` controls the whole pipeline; disabling it restores
legacy per-advert discovery alerts without changing or deleting identity,
lifecycle, or price evidence.

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

The next release uses one migration from schema version 3. It backfills one
initial price observation from every existing listing, adds the retained
matching and availability records, queues existing adverts for incremental
processing, and changes notification uniqueness from one row per
filter/listing to one row per filter/triggering event. Pre-tracking adverts
begin as unknown rather than being invented as active. Original provider
listings, prices, evidence, decisions, memberships, and worker outcomes remain
separate durable records.

The earlier development-only versions 4 through 10 were consolidated before
production deployment. A local database that already ran those unreleased
migrations must be recreated from a backup or explicitly repaired back to
version 3 before running this branch. Production databases that are still on
version 3 upgrade normally. Stop all older application instances before the
upgrade, and never run two application versions against the same Telegram bot
token.

Before cutover:

1. Back up PostgreSQL with `pg_dump`.
2. Stop the Python container; never run both applications against one bot token.
3. Start the Go image against a restored copy first and test `/start`, alert management, one source poll, and one delivery.
4. Start the Go application against production and retain the Python revision for rollback until several polling cycles succeed.

Active filters, source baselines, listings, pending notifications, and cached Telegram file IDs use the existing tables and are retained.

## Architecture

`cmd/bot` is the composition root. Pure types and matching rules live in `internal/domain`; versioned event contracts in `internal/events`; source adapters in `internal/provider`; application orchestration in `internal/app`; PostgreSQL in `internal/store/postgres`; RabbitMQ in `internal/broker/rabbitmq`; and all Telegram-specific behavior in `internal/telegram`.

The providers normalize API/RSS/detail payloads before crossing their package boundary. Discovery stores each new listing together with a `listing.discovered.v1` outbox event in one transaction. Distinct later price observations use the separate `listing.price_changed.v1` contract, with the history row, current listing price, and outbox event committed atomically. The outbox relay publishes both event types to RabbitMQ. For discoveries, the matcher waits for durable property and required availability evidence, then creates event-scoped, user-coalesced notification snapshots; existing-advert price events retain their independent path. The notifier renders only persisted listing details, media URLs, triggering prices, and active-alternative context—it never reopens a provider detail page.
