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
- [x] Tambahkan OS single-instance lock.
- [x] Tambahkan SQLite checkpoint, backup, dan integrity diagnostics.
- [x] Tambahkan migration version tracking dan upgrade tests.

## Epic 3 — OpenTUI Foundation

- [x] Buat keyboard map dan navigasi dasar.
- [x] Buat command palette.
- [x] Buat focus management, modal, confirmation, dan toast.
- [x] Buat loading, empty, error, dan cancellation states.
- [x] Buat resizable panels, log viewer, dan diff prototype.
- [x] Tambahkan terminal resize handling dan component tests.

## Epic 4 — Local Daemon Protocol

- [x] Implementasikan Unix socket transport.
- [x] Generate TypeScript client types dari schema.
- [x] Implementasikan request dan correlation IDs.
- [x] Implementasikan event stream, reconnect, dan timeline replay.
- [x] Hubungkan TUI status screen dengan daemon dan SQLite diagnostics.

## Epic 5 — Domain Persistence

- [x] Migration projects dan repositories.
- [x] Migration plans dan plan versions.
- [x] Migration planning runs dan open questions.
- [x] Migration agent configuration dan tool policy.
- [x] Migration workflow jobs.
- [x] Migration workspaces.
- [x] Migration executions, tool runs, dan patch checkpoints.
- [x] Migration check runs dan check steps.
- [x] Tambahkan check log artifact untuk output besar.
- [x] Migration review runs dan review issues.
- [x] Migration approval decisions dan revision requests.
- [x] Migration Git publications dan artifact metadata.
- [x] Tambahkan indexes, foreign keys, dan integrity rules.

User, membership, tenant, remote token, dan organization tidak termasuk MVP.

## Epic 6 — Repository and Workspace

- [x] Implementasikan local Git repository inspection.
- [x] Baca remote, branch, HEAD, dan dirty state.
- [x] Tambahkan pembacaan submodule.
- [x] Buat repository registration form dan detail screen.
- [x] Buat filesystem picker.
- [x] Buat branch selector.
- [x] Implementasikan trust level dan ignore policy.
- [x] Implementasikan local Git worktree adapter.
- [x] Tambahkan SQLite-backed workspace preparation lease.
- [x] Tambahkan managed-root, overlap, symlink, pinned-HEAD, dan dirty-state guard.
- [x] Tambahkan idempotent workspace preparation dan crash resume.
- [x] Tambahkan Executor write lease dan periodic renewal.
- [x] Tambahkan patch checkpoint dan checksum.
- [x] Tambahkan archive, cleanup, dan orphan-worktree reconciliation.

## Epic 7 — Planning and LLM Gateway

- [x] Buat requirement editor dan structured plan schema.
- [x] Implementasikan repository summary dan plan normalization.
- [x] Buat Planning Agent dan open-question flow.
- [x] Definisikan provider-neutral request, response, usage, dan error.
- [x] Implementasikan provider registry, gateway, dan fake provider deterministik.
- [x] Implementasikan provider adapter nyata pertama.
- [x] Tambahkan timeout, cancellation, retry, fallback, dan budget.
- [x] Tambahkan structured output validation dan prompt redaction.

## Epic 8 — Workflow and Scheduler

- [x] Definisikan job aggregate dan transition table.
- [x] Hubungkan workflow ke SQLite queue.
- [x] Tambahkan idempotency pada handler `workflow.prepare_workspace`.
- [x] Tambahkan idempotency pada handler `workflow.execute`.
- [x] Tambahkan idempotency pada handler `workflow.run_checks`.
- [x] Tambahkan idempotency pada handler `workflow.review`.
- [x] Tambahkan idempotency pada publication handler.
- [x] Implementasikan polling, backoff, cancellation, dan manual retry.
- [x] Pulihkan stale running job saat daemon startup.
- [x] Tambahkan revision, attempt, dan wall-clock guards.
- [x] Tambahkan crash-recovery dan duplicate-processing tests untuk workspace preparation.
- [x] Tambahkan crash-recovery dan duplicate-processing boundary untuk execution.
- [x] Tambahkan crash-recovery dan duplicate-processing tests untuk checks.
- [x] Tambahkan crash-recovery dan duplicate-processing tests untuk reviewer.
- [x] Tambahkan crash-recovery boundary untuk human revision decisions.
- [x] Tambahkan crash-recovery dan duplicate-processing tests untuk publication.

## Epic 9 — Tools and Sandbox

- [x] Implementasikan file read, search, patch, create, dan delete.
- [x] Implementasikan Git status dan diff.
- [x] Validasi canonical path, symlink, special file, dan file size.
- [x] Buat Docker sandbox adapter.
- [x] Tambahkan command profiles dan argument validation.
- [x] Tambahkan timeout, output limit, process cancellation, dan environment allowlist.
- [x] Tambahkan CPU, memory, process-count, file-size, dan tmpfs disk limits.
- [x] Disable network secara default melalui persisted tool policy.

## Epic 10 — Executor, Checks, and Reviewer

- [x] Buat execution snapshot dan deterministic scripted Executor foundation.
- [x] Buat context selector dan autonomous LLM Executor loop.
- [x] Simpan tool runs, checkpoints, changed files, dan patch.
- [x] Simpan execution token/cost usage.
- [x] Implementasikan formatter, linter, type check, test, dan build runner.
- [x] Simpan check summary dan bounded check output.
- [x] Tambahkan daemon E2E dari repository registration sampai local publication.
- [x] Simpan check log sebagai artifact terpisah.
- [x] Buat Reviewer prompt dan structured review schema.
- [x] Validasi file reference, severity, blocking flag, dan criteria.
- [x] Publish live progress setelah durable event tersimpan.

## Epic 11 — Approval and Git Publication

- [x] Buat changed-file tree dan unified diff viewer.
- [x] Buat check matrix pada Jobs screen.
- [x] Buat review issue panel read-only pada Jobs screen.
- [x] Implementasikan approve, request revision, dan reject.
- [x] Implementasikan take over dan manual workspace editing.
- [x] Bind approval ke execution version, base SHA, dan patch checksum.
- [x] Tambahkan decision composer dan Reviewer override acknowledgement pada Jobs screen.
- [x] Implementasikan local commit dengan deterministic commit message.
- [x] Buat optional GitHub adapter untuk branch push dan draft PR.
- [x] Tambahkan publication idempotency dan conflict handling.

## Epic 12 — Local Release

- [x] Implementasikan local credential storage melalui OS keychain.
- [x] Tambahkan repository, workspace, command, dan artifact security tests.
- [x] Tambahkan structured logs, local metrics, dan diagnostics bundle.
- [x] Tambahkan SQLite backup, restore, dan recovery drill.
- [x] Build daemon binary, TUI artifact, dan optional sandbox image.
- [x] Jalankan end-to-end dan security tests.
- [x] Release local MVP.

## Epic 13 — Unified Kanban Workspace

- [x] Jadikan Board sebagai layar default.
- [x] Sederhanakan navigasi utama menjadi Board, Agents, Settings, dan System.
- [x] Definisikan view model kartu dengan ID stabil berbasis plan.
- [x] Petakan seluruh plan dan workflow status ke lima kolom secara exhaustive.
- [x] Tambahkan contextual action menu tanpa drag-and-drop status.
- [x] Tambahkan responsive layout lima, tiga, dan satu kolom.
- [x] Tambahkan project filter dan active/all filter.
- [x] Pindahkan pembuatan project ke flow di dalam Board.
- [x] Gunakan Plan Editor dan Planning Question Editor dari Board.
- [x] Otomatiskan repository summary, normalization, dan Planning Agent prerequisites.
- [x] Mulai workflow dari kartu plan READY.
- [x] Lazy-load execution, checks, review, changed files, diff, dan workspace saat kartu dibuka.
- [x] Pertahankan immutable approval fingerprint dan Reviewer override.
- [x] Tambahkan approve, request revision, reject, retry, cancel, dan manual workspace dari Board.
- [x] Hubungkan durable SSE events ke refresh dan perpindahan kartu.
- [x] Pertahankan selection saat kartu berpindah kolom.
- [x] Tambahkan endpoint summary `GET /projects/:projectID/board`.
- [x] Tambahkan fallback client untuk daemon lama saat local rolling upgrade.
- [x] Tambahkan unit test status mapping dan stable identity.
- [x] Tambahkan API test untuk Board summary.
- [x] Ganti E2E lama dengan approval flow dari Unified Board.
- [x] Dokumentasikan interaction model, data loading, responsive behavior, dan safety boundary.

## Outside Current Scope

- Hosted SaaS dan multi-user control plane.
- Remote execution dan distributed workers.
- PostgreSQL, Redis, RabbitMQ, NATS, dan object storage.
- Multi-machine synchronization.
- Generic project-management fields seperti sprint, assignee, due date, story points, dan custom columns.
- Manual drag-and-drop yang dapat melewati workflow state machine.
