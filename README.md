# Orkoda

Orkoda is a terminal-native orchestrator for AI-assisted software development. A developer provides a requirement, a planning agent turns it into an implementation plan, an executor works in an isolated Git workspace, automated checks and a reviewer validate the result, and a human approves the immutable diff before publication.

## Repository status

The repository currently contains the Phase 1 foundation:

- OpenTUI React shell under `apps/tui`.
- Go local daemon, worker, and migration command entry points.
- Shared versioned JSON protocol schema.
- Local PostgreSQL, Redis, and RabbitMQ infrastructure.
- Local filesystem artifact storage under `.orkoda/artifacts`.
- Formatting, linting, tests, Make targets, and GitHub Actions CI.

Product and implementation documents are available in [`docs/`](./docs/README.md).

## Requirements

- Go 1.26+
- Bun 1.3.14+
- Docker with Compose v2

OpenTUI uses Bun as the supported native runtime for the TUI renderer.

## Getting started

```bash
cp .env.example .env
make install
make dev-up
```

Run the local API daemon:

```bash
make api
```

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

Orkoda is designed to run locally for personal use. Runtime data is stored under `.orkoda/` by default:

```text
.orkoda/
├── artifacts/
├── workspaces/
└── logs/
```

The directories can be changed with `ORKODA_DATA_DIR` and `ORKODA_ARTIFACT_DIR`. The entire `.orkoda/` directory is ignored by Git.

## Quality checks

```bash
make check
```

Individual commands are available through `make help`.

## Repository layout

```text
apps/tui/              OpenTUI + React client
cmd/api/               Go API and local daemon
cmd/worker/            Executor/reviewer worker process
cmd/migrate/           Database migration command
internal/              Go application packages
packages/protocol/     Shared versioned JSON schemas
docs/                  Product, architecture, and implementation docs
```

## Safety boundary

Orkoda must never edit a developer's source repository directly. Future execution workspaces will use Git worktrees or isolated clones tied to an immutable base commit. Commit, push, or pull-request publication remains blocked until explicit human approval.
