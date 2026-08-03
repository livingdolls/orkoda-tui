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
- A provider-neutral LLM gateway with local fake and configurable OpenAI-compatible providers.
- Bounded LLM retries, explicit fallbacks, cancellation, backoff, and token budgets.
- Prompt redaction, structured response validation, and bounded output repair.
- Formatting, linting, tests, Make targets, and GitHub Actions CI.

Orkoda does not require PostgreSQL, Redis, RabbitMQ, MinIO, or Docker Compose.

Product and implementation documents are available in [`docs/`](./docs/README.md).

## Requirements

- Go 1.26+
- Bun 1.3.14+

Docker is required for the default isolated check runner. Host check execution is available only as an explicit development escape hatch with `ORKODA_SANDBOX_MODE=host` and `ORKODA_ALLOW_UNSANDBOXED_CHECKS=true`.

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

The daemon also applies idempotent SQLite migrations automatically during startup. Planning, execution, checks, review, and publication run as durable background jobs inside this same process.

In another terminal, run the TUI:

```bash
make tui
```

The API exposes:

```text
GET /health/live
GET /api/v1/status
GET /api/v1/llm/providers
GET /api/v1/llm/policy
```

`/health/live` is public. Every `/api/v1` request requires the bearer token in
`.orkoda/api.token` (or the token configured through `ORKODA_API_TOKEN`).

## LLM providers

Orkoda uses `local-fake` by default, so the complete planning and open-question flow works without credentials or network access.

To use an OpenAI-compatible endpoint, configure the daemon environment before startup:

```text
ORKODA_LLM_PROVIDER=openrouter
ORKODA_LLM_BASE_URL=https://provider.example/v1
ORKODA_LLM_API_KEY=your-secret
ORKODA_LLM_MODEL=provider/model-name
ORKODA_LLM_JSON_MODE=json_schema
ORKODA_LLM_TIMEOUT=60s
ORKODA_LLM_HEADERS_JSON={"X-Title":"Orkoda"}
```

`ORKODA_LLM_JSON_MODE` accepts `json_schema`, `json_object`, or `prompt_only`. HTTPS is required except for loopback development endpoints such as `http://127.0.0.1:11434/v1`. Credentials remain in process memory and are never returned by the provider status API or stored in SQLite.

### Resilience and budget policy

One logical LLM request can be bounded independently from the provider HTTP client:

```text
ORKODA_LLM_ATTEMPT_TIMEOUT=45s
ORKODA_LLM_MAX_WALL_CLOCK=2m
ORKODA_LLM_MAX_ATTEMPTS=3
ORKODA_LLM_BACKOFF_INITIAL=500ms
ORKODA_LLM_BACKOFF_MAX=8s
ORKODA_LLM_BACKOFF_JITTER=0.2
ORKODA_LLM_MAX_INPUT_TOKENS=50000
ORKODA_LLM_MAX_OUTPUT_TOKENS=8000
ORKODA_LLM_MAX_TOTAL_TOKENS=60000
ORKODA_LLM_FALLBACKS_JSON=[]
```

Only `RATE_LIMITED`, `TIMEOUT`, and `UNAVAILABLE` errors are retried. Authentication, invalid request, context-length, budget, and cancellation errors fail immediately. `Retry-After` takes precedence over local exponential backoff.

Fallbacks are explicit and must reference a provider already registered by the daemon. `local-fake` is never selected silently; it is used as a fallback only when it is listed explicitly, for example:

```text
ORKODA_LLM_FALLBACKS_JSON=[{"provider":"local-fake","model":"local-fake-planner-v1"}]
```

### Prompt and structured-output safety

High-confidence secrets are redacted before token estimation and before a request reaches any provider. Structured responses are limited by size, parsed as exactly one JSON value, checked against the supplied schema, and then checked by Planning Agent domain validation.

```text
ORKODA_LLM_REDACTION_MODE=strict
ORKODA_LLM_MAX_REPAIR_ATTEMPTS=1
ORKODA_LLM_MAX_STRUCTURED_RESPONSE_BYTES=1048576
```

Redaction modes:

- `strict`: replace high-confidence secrets with stable request-local placeholders.
- `report`: detect and count secrets without changing the prompt.
- `off`: disable prompt redaction for local debugging only.

A failed structured response can trigger a bounded repair request. The repair prompt contains the original redacted request and safe validation issue paths, but never includes the malformed provider response. Repair calls share the parent wall-clock limit and are checked against the remaining token budget.

The Settings screen lists registered providers, execution policy, prompt-redaction mode, repair limit, and maximum structured-response size. Planning-run usage persists total tokens, provider attempts, fallback state, validation attempts, repair state, and redaction count.

## Local data

Runtime data is stored under `.orkoda/` by default:

```text
.orkoda/
├── orkoda.db
├── orkoda.db-wal
├── orkoda.db-shm
├── artifacts/
├── workspaces/
├── api.token
├── orkoda.db.bak
└── logs/
```

Configuration:

```text
ORKODA_DATA_DIR=.orkoda
ORKODA_DATABASE_PATH=.orkoda/orkoda.db
ORKODA_ARTIFACT_DIR=.orkoda/artifacts
ORKODA_API_TOKEN_FILE=.orkoda/api.token
ORKODA_SANDBOX_MODE=docker
ORKODA_SANDBOX_IMAGE=orkoda-sandbox:local
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
internal/llm/          Provider-neutral gateway and provider adapters
internal/              Remaining Go application packages
packages/protocol/     Shared versioned JSON schemas
docs/                  Product, architecture, and implementation docs
```

## Safety boundary

Orkoda never edits a developer's source repository directly. Execution uses an isolated Git worktree tied to an immutable base commit. Local commit publication remains blocked until explicit human approval; remote push and pull-request adapters remain outside the MVP.
