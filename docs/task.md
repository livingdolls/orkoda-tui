# Development Tasks

Backlog Orkoda untuk penggunaan personal pada satu komputer.

## Epic 1 — Foundation

- [x] Setup Go module dan OpenTUI React workspace.
- [x] Setup shared protocol schema, Makefile, test, lint, dan CI.
- [x] Tambahkan `.orkoda/` sebagai local runtime root.
- [x] Tambahkan filesystem artifact storage.
- [x] Ganti PostgreSQL dengan embedded SQLite tanpa CGO.
- [x] Hapus Redis, RabbitMQ, MinIO, dan Docker Compose.
- [x] Tambahkan SQLite WAL configuration dan migration bootstrap.
- [x] Tambahkan durable SQLite job queue.
- [x] Tambahkan retry, dead status, dan stale-job recovery API.
- [x] Tambahkan in-memory event bus.

## Epic 2 — Local Runtime Completion

- [x] Jalankan queue scheduler sebagai goroutine daemon.
- [x] Tambahkan activity event repository dan replay berdasarkan sequence.
- [x] Tambahkan coordinated goroutine shutdown.
- [ ] Tambahkan OS single-instance lock.
- [ ] Tambahkan SQLite checkpoint, backup, dan integrity diagnostics.
- [ ] Tambahkan migration version tracking dan upgrade tests.

## Epic 3 — OpenTUI Foundation

- [x] Buat keyboard map dan navigasi dasar.
- [ ] Buat command palette.
- [ ] Buat focus management, modal, confirmation, dan toast.
- [ ] Buat loading, empty, error, dan cancellation states.
- [ ] Buat resizable panels, log viewer, dan diff prototype.
- [ ] Tambahkan terminal resize handling dan component tests.

## Epic 4 — Local Daemon Protocol

- [ ] Implementasikan Unix socket transport.
- [ ] Generate TypeScript client types dari schema.
- [ ] Implementasikan request dan correlation IDs.
- [ ] Implementasikan event stream, reconnect, dan timeline replay.
- [ ] Hubungkan TUI status screen dengan daemon dan SQLite diagnostics.

## Epic 5 — Domain Persistence

- [x] Migration projects dan repositories.
- [x] Migration plans dan plan versions.
- [x] Migration planning runs dan open questions.
- [ ] Migration agent configuration dan tool policy.
- [ ] Migration workflow jobs dan workspaces.
- [ ] Migration executions, tool runs, dan checks.
- [ ] Migration reviews, revisions, dan approvals.
- [ ] Migration Git publications dan artifact metadata.
- [ ] Tambahkan indexes, foreign keys, dan integrity rules.

User, membership, tenant, remote token, dan organization tidak termasuk MVP.

## Epic 6 — Repository and Workspace

- [x] Implementasikan local Git repository inspection.
- [x] Baca remote, branch, HEAD, dan dirty state.
- [ ] Tambahkan pembacaan submodule.
- [x] Buat repository registration form dan detail screen.
- [x] Buat filesystem picker.
- [ ] Buat branch selector.
- [ ] Implementasikan trust level dan ignore policy.
- [ ] Implementasikan local Git worktree adapter.
- [ ] Tambahkan SQLite-backed workspace lease.
- [ ] Tambahkan path guard, patch checkpoint, cleanup, dan recovery.

## Epic 7 — Planning and LLM Gateway

- [x] Buat requirement editor dan structured plan schema.
- [x] Implementasikan repository summary dan plan normalization.
- [x] Buat Planning Agent dan open-question flow.
- [x] Definisikan provider-neutral request, response, usage, dan error.
- [x] Implementasikan provider registry, gateway, dan fake provider deterministik.
- [x] Implementasikan provider adapter nyata pertama.
- [ ] Tambahkan timeout, cancellation, retry, fallback, dan budget.
- [ ] Tambahkan structured output validation dan prompt redaction.

## Epic 8 — Workflow and Scheduler

- [ ] Definisikan job aggregate dan transition table.
- [ ] Hubungkan workflow ke SQLite queue.
- [ ] Tambahkan handler idempotency.
- [ ] Implementasikan polling, backoff, cancellation, dan manual retry.
- [x] Pulihkan stale running job saat daemon startup.
- [ ] Tambahkan revision, attempt, dan wall-clock guards.
- [ ] Tambahkan crash-recovery dan duplicate-processing tests.

## Epic 9 — Tools and Sandbox

- [ ] Implementasikan file read, search, patch, create, dan delete.
- [ ] Implementasikan Git status dan diff.
- [ ] Validasi canonical path, symlink, dan file size.
- [ ] Buat Docker sandbox adapter.
- [ ] Tambahkan command profiles dan argument validation.
- [ ] Tambahkan CPU, memory, process, disk, timeout, dan output limits.
- [ ] Disable network secara default.
- [ ] Implementasikan process-tree cancellation dan environment allowlist.

## Epic 10 — Executor, Checks, and Reviewer

- [ ] Buat execution snapshot, context selector, dan Executor loop.
- [ ] Simpan tool runs, checkpoints, changed files, patch, dan usage.
- [ ] Implementasikan formatter, linter, type check, test, dan build runner.
- [ ] Simpan check summary dan log artifact.
- [ ] Buat Reviewer prompt dan structured review schema.
- [ ] Validasi file reference, severity, blocking flag, dan criteria.
- [ ] Publish live progress setelah durable event tersimpan.

## Epic 11 — Approval and Git Publication

- [ ] Buat changed-file tree dan unified diff viewer.
- [ ] Buat check matrix dan review issue panel.
- [ ] Implementasikan approve, revision, reject, dan take over.
- [ ] Bind approval ke execution version, base SHA, dan patch checksum.
- [ ] Implementasikan local commit dan commit message editor.
- [ ] Buat optional GitHub adapter untuk branch push dan draft PR.
- [ ] Tambahkan publication idempotency dan conflict handling.

## Epic 12 — Local Release

- [ ] Implementasikan local credential storage melalui OS keychain.
- [ ] Tambahkan repository, workspace, command, dan artifact security tests.
- [ ] Tambahkan structured logs, local metrics, dan diagnostics bundle.
- [ ] Tambahkan SQLite backup, restore, dan recovery drill.
- [ ] Build daemon binary, TUI artifact, dan optional sandbox image.
- [ ] Jalankan end-to-end dan security tests.
- [ ] Release local MVP.

## Outside Current Scope

- Hosted SaaS dan multi-user control plane.
- Remote execution dan distributed workers.
- PostgreSQL, Redis, RabbitMQ, NATS, dan object storage.
- Multi-machine synchronization.
