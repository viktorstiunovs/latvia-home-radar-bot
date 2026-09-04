# Latvia Home Radar project guide

## Purpose and stack

- This is a Go Telegram bot that monitors SS.lv and City24.lv for
  Latvian apartment and house listings and notifies users when saved filters
  match.
- The module requires Go 1.26 or newer. PostgreSQL 17 stores application state,
  RabbitMQ 4 transports listing events, and Goose migrations are embedded in
  the application binary.
- `cmd/bot/main.go` is the composition root. Keep business logic out of it; use
  it to construct dependencies, start workers, and coordinate shutdown.

## Package map

- `internal/domain`: provider-independent entities and matching rules.
- `internal/provider`: source boundary; `sslv` and `city24` normalize external
  payloads before returning domain listings.
- `internal/app`: orchestration services and consumer-owned interfaces.
- `internal/store/postgres`: PostgreSQL adapters and transaction boundaries.
- `internal/broker/rabbitmq`: RabbitMQ topology, publishing, and consumption.
- `internal/events`: versioned integration-event contracts.
- `internal/telegram`: bot interaction, alert wizard, formatting, and delivery.
- `migrations`: ordered SQL migrations embedded through `migrations/embed.go`.

## Important behavior

- A source's first successful poll establishes a baseline and must not notify
  users about existing listings.
- Persisting a discovered listing and its outbox event is one PostgreSQL
  transaction. Do not replace this with a database write followed directly by
  a RabbitMQ publish.
- The outbox relay waits for a RabbitMQ publisher confirmation before marking
  an event as published.
- Listing matching is idempotent by event ID. Preserve that property when
  changing retries or consumer behavior.
- `listing.discovered.v1` is a versioned external contract. Do not change its
  meaning incompatibly; introduce a new event version instead.
- Notifications operate only on persisted listing details and media URLs; they
  must not fetch provider detail pages again.
- Migrations run at startup. Add a new sequential migration for schema changes
  rather than rewriting a migration that may already have been applied.
- Never run two application instances against the same Telegram bot token
  during migration or cutover.

## Local development

- Copy `.env.example` to `.env` and set `TELEGRAM_BOT_TOKEN` and
  `SCRAPER_CONTACT`. Never commit `.env` or expose its secrets in logs, tests,
  or documentation.
- `config.FromEnv` reads process environment variables; it does not load a
  `.env` file itself. Configure the IDE run configuration accordingly.
- Start infrastructure with `docker compose up -d db rabbitmq`.
- Run the application with `go run ./cmd/bot`.
- Run the standard test suite with `go test ./...`.
- PostgreSQL integration tests require `TEST_DATABASE_URL` and deliberately
  refuse to truncate a database whose name does not end in `_test`.
- RabbitMQ integration tests require `TEST_RABBITMQ_URL`. Tests without these
  variables skip their integration portions.

## Go implementation guidelines

- Run `gofmt` on every modified Go file and run the relevant tests before
  considering a change complete.
- Never place a function or method body on the same line as its signature, even
  for a trivial one-line implementation.

  ```go
  // Do not use:
  func enabled() bool { return true }

  // Use:
  func enabled() bool {
      return true
  }
  ```

- Put one empty line after every top-level function or method declaration,
  before the next declaration.
- Use blank lines to separate logical setup or processing phases inside longer
  functions.
- Keep `context.Context` as the first parameter for cancellable operations and
  propagate cancellation to blocking I/O and worker loops.
- Wrap errors with useful operation context and `%w` when callers may need the
  underlying error.
- Define small interfaces in the consuming package. Go interface satisfaction
  is implicit; do not add coupling solely to assert an implementation.
- Keep tests beside their source using `_test.go`. Prefer fixtures and
  deterministic tests over live provider requests.
- Make goroutine ownership and termination explicit. Avoid starting a
  goroutine without a cancellation path or a bounded result channel.
- Preserve concurrency safety around shared RabbitMQ channels and publisher
  confirmations.

## Releases

- Record notable work under `Unreleased` in `CHANGELOG.md`.
- Release with semantic-version Git tags such as `v0.2.0`; move the accumulated
  changelog entries into a dated version section when tagging.

<!-- BACKLOG.MD MCP GUIDELINES START -->
<!-- backlog.md-instructions-version: 1.51.0 -->

<CRITICAL_INSTRUCTION>

## BACKLOG WORKFLOW INSTRUCTIONS

This project uses Backlog.md MCP for all task and project management activities.

**CRITICAL GUIDANCE**

- If your client supports MCP resources, read `backlog://workflow/overview` to understand when and how to use Backlog for this project.
- If your client only supports tools or the above request fails, call `backlog.get_backlog_instructions()` to load the tool-oriented overview. Use the `instruction` selector when you need `task-creation`, `task-execution`, or `task-finalization`.

- **First time working here?** Read the overview resource IMMEDIATELY to learn the workflow
- **Already familiar?** You should have the overview cached ("## Backlog.md Overview (MCP)")
- **When to read it**: BEFORE creating tasks, or when you're unsure whether to track work

These guides cover:
- Decision framework for when to create tasks
- Search-first workflow to avoid duplicates
- Links to detailed guides for task creation, execution, and finalization
- MCP tools reference

You MUST read the overview resource to understand the complete workflow. The information is NOT summarized here.

</CRITICAL_INSTRUCTION>

<!-- BACKLOG.MD MCP GUIDELINES END -->

