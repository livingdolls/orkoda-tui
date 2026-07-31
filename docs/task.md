# Development Tasks

Backlog ini khusus untuk AI software development workflow berbasis OpenTUI.

## Epic 1 — Repository and Tooling

- [ ] Buat repository utama.
- [ ] Setup Go workspace.
- [ ] Setup TypeScript OpenTUI application.
- [ ] Pilih `@opentui/core` atau `@opentui/react` dan dokumentasikan keputusan.
- [ ] Setup shared protocol schema.
- [ ] Tambahkan Docker Compose untuk PostgreSQL, Redis, dan RabbitMQ.
- [ ] Tambahkan local filesystem artifact storage.
- [ ] Tambahkan Makefile dan task runner.
- [ ] Tambahkan `.env.example` tanpa secret.
- [ ] Tambahkan formatter, linter, test, dan CI dasar.

## Epic 2 — OpenTUI Foundation

- [ ] Buat app shell dan route/state navigation.
- [ ] Buat keyboard map dan command palette.
- [ ] Buat focus management.
- [ ] Buat modal, confirmation, toast, loading, empty, dan error components.
- [ ] Buat resizable panels.
- [ ] Buat log viewer dengan pagination.
- [ ] Buat diff viewer prototype.
- [ ] Buat diagnostics screen.
- [ ] Tambahkan terminal resize dan small-screen handling.
- [ ] Tambahkan component and snapshot tests.

## Epic 3 — API and Local Daemon

- [ ] Buat Golang API/local daemon.
- [ ] Implementasikan Unix socket transport.
- [ ] Implementasikan HTTP transport untuk remote mode.
- [ ] Definisikan versioned protocol envelope.
- [ ] Generate TypeScript client types dari schema.
- [ ] Implementasikan request/correlation IDs.
- [ ] Implementasikan event stream dan reconnect.
- [ ] Implementasikan graceful shutdown.

## Epic 4 — Database Foundation

- [ ] Migration users, projects, repositories, and memberships.
- [ ] Migration plans and plan_versions.
- [ ] Migration agents and tool policies.
- [ ] Migration jobs and workspaces.
- [ ] Migration executions and tool_runs.
- [ ] Migration check_definitions and check_runs.
- [ ] Migration reviews and review_issues.
- [ ] Migration revision_requests and approvals.
- [ ] Migration git_publications and artifacts.
- [ ] Migration activity_events, outbox_events, and processed_messages.
- [ ] Tambahkan indexes and integrity constraints.
- [ ] Tambahkan migration integration test.

## Epic 5 — Identity and Authorization

- [ ] Implementasikan local profile.
- [ ] Implementasikan remote device login.
- [ ] Implementasikan access and refresh tokens.
- [ ] Simpan credential melalui keychain adapter.
- [ ] Implementasikan project membership.
- [ ] Implementasikan permissions: view, execute, approve, credentials, publish.
- [ ] Tambahkan audit events dan auth integration tests.

## Epic 6 — Repository Management

- [ ] Buat Repository entity and adapter interfaces.
- [ ] Implementasikan local Git repository inspection.
- [ ] Baca remote, branches, HEAD, dirty state, and submodules.
- [ ] Implementasikan repository trust levels.
- [ ] Buat repository picker and detail screens.
- [ ] Buat branch selector.
- [ ] Tambahkan ignore and secret path policy.
- [ ] Tambahkan repository fixture tests.

## Epic 7 — Workspace Manager

- [ ] Buat Workspace entity and lifecycle.
- [ ] Implementasikan Git worktree adapter.
- [ ] Implementasikan isolated clone adapter.
- [ ] Tambahkan write lease.
- [ ] Tambahkan path and symlink guards.
- [ ] Simpan base commit SHA and patch checksum.
- [ ] Implementasikan patch checkpoint.
- [ ] Implementasikan archive, cleanup, and orphan recovery.
- [ ] Tambahkan concurrency and recovery tests.

## Epic 8 — Planning

- [ ] Buat requirement editor di OpenTUI.
- [ ] Definisikan structured plan JSON Schema.
- [ ] Buat Planning Agent prompt.
- [ ] Implementasikan repository summary builder.
- [ ] Implementasikan plan normalization.
- [ ] Buat affected-area and test-strategy panels.
- [ ] Buat open-question flow.
- [ ] Implementasikan plan version and accept action.

## Epic 9 — LLM Gateway

- [ ] Definisikan canonical request, response, usage, and errors.
- [ ] Implementasikan provider interface.
- [ ] Implementasikan provider adapter pertama.
- [ ] Implementasikan fake provider.
- [ ] Implementasikan timeout, cancellation, retry, and fallback policy.
- [ ] Implementasikan structured output validation.
- [ ] Implementasikan token/cost accounting.
- [ ] Implementasikan prompt redaction and source delimiters.

## Epic 10 — Workflow and Messaging

- [ ] Definisikan Job aggregate and status enums.
- [ ] Implementasikan transition table and optimistic locking.
- [ ] Implementasikan transactional outbox.
- [ ] Setup RabbitMQ exchanges and queues.
- [ ] Implementasikan processed message store.
- [ ] Implementasikan retry, backoff, manual ack, and DLQ.
- [ ] Implementasikan budget, revision, and wall-clock guards.
- [ ] Tambahkan table-driven and duplicate-delivery tests.

## Epic 11 — File Tools

- [ ] Implementasikan file read.
- [ ] Implementasikan glob and semantic/text search abstraction.
- [ ] Implementasikan file patch.
- [ ] Implementasikan file create and delete.
- [ ] Implementasikan Git status and diff.
- [ ] Validasi canonical path and file size.
- [ ] Simpan tool input redacted and result summary.
- [ ] Tambahkan malicious path tests.

## Epic 12 — Sandbox and Command Runner

- [ ] Buat Sandbox interface.
- [ ] Implementasikan Docker sandbox adapter.
- [ ] Buat command profiles per language/toolchain.
- [ ] Implementasikan executable and argument validation.
- [ ] Tambahkan CPU, memory, PID, disk, timeout, and output limits.
- [ ] Disable network by default.
- [ ] Implementasikan process tree cancellation.
- [ ] Tambahkan environment allowlist and secret broker.
- [ ] Jalankan sandbox security test suite.

## Epic 13 — Executor Worker

- [ ] Buat execution input snapshot.
- [ ] Buat context file selector.
- [ ] Buat Executor prompt builder.
- [ ] Implementasikan model/tool loop.
- [ ] Simpan tool runs and checkpoints.
- [ ] Simpan changed files, patch, summary, and usage.
- [ ] Publish live progress events.
- [ ] Tangani timeout, cancellation, provider failure, and policy violation.
- [ ] Tambahkan worker restart test.

## Epic 14 — Automated Checks

- [ ] Buat check definition management.
- [ ] Implementasikan formatter runner.
- [ ] Implementasikan linter runner.
- [ ] Implementasikan type check runner.
- [ ] Implementasikan unit/integration test runner.
- [ ] Implementasikan build runner.
- [ ] Simpan exit code, duration, summary, and log artifact.
- [ ] Buat check result screen.
- [ ] Implementasikan required-check policy and retry.

## Epic 15 — Reviewer Worker

- [ ] Definisikan code review schema.
- [ ] Buat Reviewer prompt builder.
- [ ] Siapkan read-only workspace snapshot.
- [ ] Validasi file and line references.
- [ ] Validasi severity, category, blocking flag, and criterion IDs.
- [ ] Simpan review and issues.
- [ ] Implementasikan review retry and schema repair.
- [ ] Tambahkan prompt regression suite.

## Epic 16 — Diff and Review TUI

- [ ] Buat changed-file tree.
- [ ] Buat unified diff viewer.
- [ ] Buat file and hunk navigation.
- [ ] Buat check matrix.
- [ ] Buat review issue panel.
- [ ] Jump dari issue ke diff location.
- [ ] Buat execution version comparison.
- [ ] Tambahkan large-diff pagination.

## Epic 17 — Human Approval and Revision

- [ ] Implementasikan approve.
- [ ] Implementasikan approve with override reason.
- [ ] Implementasikan request revision.
- [ ] Implementasikan reject and take over.
- [ ] Bind approval to execution version, base SHA, and patch checksum.
- [ ] Buat selected-issue revision input.
- [ ] Buat approval confirmation dialog.
- [ ] Tambahkan concurrent approval and checksum mismatch tests.

## Epic 18 — Git Publication

- [ ] Implementasikan local commit.
- [ ] Implementasikan commit message editor.
- [ ] Buat Git provider interface.
- [ ] Implementasikan provider adapter pertama.
- [ ] Implementasikan branch push.
- [ ] Implementasikan draft pull request.
- [ ] Block protected branch and force push.
- [ ] Implementasikan publication idempotency and conflict handling.
- [ ] Buat publication result screen.

## Epic 19 — Security Hardening

- [ ] Implementasikan secret scanning and redaction.
- [ ] Implementasikan repository trust policy.
- [ ] Audit path, symlink, and workspace isolation.
- [ ] Audit command profiles and network policy.
- [ ] Encrypt provider and Git credentials.
- [ ] Tambahkan rate, concurrency, and cost limits.
- [ ] Tambahkan malicious repository fixtures.
- [ ] Tambahkan approval integrity audit.

## Epic 20 — Observability and Release

- [ ] Tambahkan structured logs and OpenTelemetry.
- [ ] Tambahkan workflow, queue, tool, sandbox, provider, and Git metrics.
- [ ] Buat OpenTUI diagnostics and redacted debug bundle.
- [ ] Setup staging remote environment.
- [ ] Setup backups and recovery drill.
- [ ] Build/sign Go binaries, TUI artifacts, and container images.
- [ ] Jalankan end-to-end, load, and security tests.
- [ ] Siapkan incident runbook.
- [ ] Release MVP.

## Post-MVP

- [ ] Language Server Protocol integration.
- [ ] Symbol and dependency graph.
- [ ] Test impact analysis.
- [ ] Multi-agent specialist teams.
- [ ] IDE companion extension.
- [ ] Self-hosted enterprise control plane.
