# Orkoda

Orkoda is a terminal-native orchestrator for AI-assisted software development. A developer provides a requirement, a planning agent turns it into an implementation plan, an executor works in an isolated Git workspace, automated checks and a reviewer validate the result, and a human approves the immutable diff before publication.

## Repository status

The repository currently contains the Phase 1 foundation:

- OpenTUI React shell under `apps/tui`.
- A single Go local daemon and migration command.
- Embedded SQLite persistence with WAL mode.
- A durable SQLite-backed job queue with retry and dead-job handling.
- An in-memory event bus for live TUI updates.
- Local filesystem artifact storage under `.orkoda/artifacts`.
- Shared versioned JSON protocol schema.
- Formatting, linting, tests, Make targets, and GitHub Actions CI.

Orkoda does not require PostgreSQL, Redis, RabbitMQ, MinIO, or Docker Compose.

Product and implementation documents are available in [`docs/`](./docs/README.md).

## Requirements

- Go 1.26+
- Bun 1.3.14+

Docker is optional and will only be needed by the future command sandbox, not by the Orkoda daemon itself.

## Getting started

```bash
cp .env.example .env
make install
make migrate
```

Run the local daemon:

```bash
make api
```

The daemon also applies idempotent SQLite migrations automatically during startup. Planning, execution, checks, and review will run as background goroutines inside this same process as their workflow handlers are implemented.

In another terminal, run the TUI:

```bash
make tui
```

The API exposes:

```text
GET /health/live
GET /api/v1/status
```

## Local data

Runtime data is stored under `.orkoda/` by default:

```text
.orkoda/
├── orkoda.db
├── orkoda.db-wal
├── orkoda.db-shm
├── artifacts/
├── workspaces/
└── logs/
```

Configuration:

```text
ORKODA_DATA_DIR=.orkoda
ORKODA_DATABASE_PATH=.orkoda/orkoda.db
ORKODA_ARTIFACT_DIR=.orkoda/artifacts
```

The entire `.orkoda/` directory is ignored by Git. Run `make clean-data` only when you intentionally want to delete all local Orkoda state.

## Quality checks

```bash
make check
```

Individual commands are available through `make help`.

## Repository layout

```text
apps/tui/              OpenTUI + React client
cmd/api/               Go local daemon
cmd/migrate/           SQLite migration command
internal/artifact/     Local filesystem artifact storage
internal/database/     SQLite bootstrap and migrations
internal/eventbus/     In-process live event delivery
internal/jobqueue/     Durable SQLite job queue
internal/              Remaining Go application packages
packages/protocol/     Shared versioned JSON schemas
docs/                  Product, architecture, and implementation docs
```

## Safety boundary

Orkoda must never edit a developer's source repository directly. Future execution workspaces will use Git worktrees or isolated clones tied to an immutable base commit. Commit, push, or pull-request publication remains blocked until explicit human approval.
