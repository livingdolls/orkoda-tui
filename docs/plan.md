# Implementation Plan

## 1. Prinsip Implementasi

- Produk hanya melayani workflow software development.
- Produk digunakan secara personal dan berjalan pada satu komputer.
- OpenTUI adalah interface utama; domain tidak bergantung pada UI.
- Satu Golang daemon menjalankan API, scheduler, dan agent workers.
- SQLite menjadi source of truth sekaligus durable job queue.
- Cache dan live event bersifat in-memory; durable timeline tetap di SQLite.
- Repository tidak pernah diedit langsung tanpa isolated workspace.
- File write dan command execution hanya aktif setelah sandbox policy tersedia.
- Executor, automated checks, Reviewer, dan Human Approval tetap menjadi tahap terpisah secara domain.
- Publish ke Git hanya dilakukan dari execution version yang approved.

## 2. Strategi Delivery

1. Foundation OpenTUI, daemon, SQLite, artifact storage, dan local queue.
2. Repository inspection dan isolated workspace.
3. Workflow state machine dan Planning Agent.
4. Executor tools dan Docker sandbox.
5. Automated checks dan Reviewer.
6. Human approval, revision, commit, dan pull request.
7. Reliability, security, observability, dan local distribution.

## 3. Phase 1 — Local Foundation and OpenTUI

### Hasil

- TUI dapat dijalankan dan terhubung ke local daemon.
- SQLite dibuat dan dimigrasikan otomatis.
- Durable queue, live event bus, local artifact storage, config, logging, dan quality gates tersedia.

### Pekerjaan

- Setup TypeScript workspace untuk `@opentui/react` dan Bun.
- Buat app shell, keyboard map, focus management, modal, toast, dan error boundary.
- Setup Golang local daemon dengan graceful shutdown.
- Setup SQLite WAL, migration, durable jobs, dan activity timeline.
- Setup local filesystem artifact storage.
- Definisikan versioned protocol antara TUI dan daemon.
- Buat repository list, project detail, settings, dan job list screens.

Tidak ada PostgreSQL, Redis, RabbitMQ, MinIO, Docker Compose, remote authentication, atau distributed worker pada fase ini.

## 4. Phase 2 — Repository and Workspace

### Hasil

- Repository lokal dapat diinspeksi tanpa diubah.
- Setiap job memperoleh Git worktree terisolasi dari base commit tertentu.

### Pekerjaan

- Repository discovery dan validation.
- Git remote, branch, commit, dirty state, dan submodule detection.
- Git worktree adapter.
- Workspace lifecycle: prepare, ready, locked, archived, cleanup.
- SQLite-backed workspace lease.
- File index dan ignore policy.
- Patch snapshot dan workspace recovery.
- Restricted mode untuk repository yang belum dipercaya.

## 5. Phase 3 — Workflow and LLM Gateway

### Hasil

- Requirement dapat dinormalisasi dan workflow berjalan melalui SQLite queue.

### Pekerjaan

- Implementasikan Job aggregate dan transition table.
- Tambahkan optimistic version dan idempotency key pada side effect penting.
- Definisikan canonical LLM request/response.
- Implementasikan provider adapter pertama dan fake provider.
- Buat Planning Agent untuk menghasilkan affected areas, steps, tests, dan criteria.
- Tambahkan token/cost budget, cancellation, retry, dan timeout.
- Jalankan queue polling dan stale-job recovery sebagai goroutine daemon.
- Stream live event ke TUI dan replay timeline dari SQLite.

## 6. Phase 4 — Executor and Tool Runtime

### Hasil

- Executor dapat melakukan perubahan source code yang dapat diaudit.

### Pekerjaan

- File read, glob, search, patch, create, dan delete tools.
- Git status dan diff tools.
- Docker sandboxed command runner.
- Per-command timeout, output limit, env allowlist, network policy, dan working directory guard.
- Tool result schema dan audit record.
- Executor loop dengan maximum iteration.
- Context builder yang hanya mengirim file relevan.
- Checkpoint patch setelah perubahan penting.

## 7. Phase 5 — Automated Checks and Reviewer

### Hasil

- Setiap execution memiliki evidence teknis dan review independen.

### Pekerjaan

- Project check configuration.
- Formatter, linter, type check, test, build, dan custom command runner.
- Check result parser dan log artifact.
- Reviewer prompt khusus code review.
- Issue category: correctness, security, performance, maintainability, testing, compatibility, dan scope.
- File/line references dan evidence validation.
- Blocking issue policy.
- Read-only reviewer workspace.

## 8. Phase 6 — Approval, Revision, and Git Publication

### Hasil

- User mengontrol hasil akhir dari terminal.

### Pekerjaan

- Diff viewer per file dan hunk.
- Review issue panel dan acceptance criteria matrix.
- Approve, approve with notes, request revision, reject, dan take over actions.
- Bind approval ke execution version, base SHA, dan diff checksum.
- Revision context builder.
- Commit message generator dan local commit.
- Optional GitHub provider adapter untuk branch push dan draft pull request.
- Publication idempotency dan conflict detection.

## 9. Phase 7 — Local Release Readiness

### Reliability

- Graceful shutdown seluruh goroutine.
- Queue retry, dead job, dan stale lock recovery.
- Workspace orphan recovery.
- SQLite checkpoint, backup, restore, dan integrity check.

### Security

- Secret scanning sebelum model input dan publication.
- Symlink/path traversal defense.
- Sandbox hardening.
- Repository trust policy.
- Local credential storage melalui OS keychain bila tersedia.

### Observability

- Structured logs dan correlation IDs.
- Local workflow, queue, provider, sandbox, dan Git metrics.
- TUI diagnostics screen dan redacted debug bundle.

### Distribution

- Satu Go daemon binary.
- OpenTUI Bun application atau bundled executable.
- Optional Docker sandbox image.
- Tidak ada server deployment atau external service bundle.

## 10. Struktur Repository

```text
orkoda-tui/
├── apps/
│   └── tui/                    # OpenTUI + TypeScript
├── cmd/
│   ├── api/                    # Local daemon
│   └── migrate/                # Explicit SQLite migration command
├── internal/
│   ├── artifact/
│   ├── database/
│   ├── eventbus/
│   ├── jobqueue/
│   ├── project/
│   ├── repository/
│   ├── planning/
│   ├── workflow/
│   ├── workspace/
│   ├── execution/
│   ├── review/
│   ├── approval/
│   ├── publication/
│   ├── llm/
│   ├── tools/
│   ├── sandbox/
│   └── observability/
├── packages/
│   ├── protocol/
│   └── prompts/
├── docs/
├── Makefile
└── README.md
```

## 11. Definition of Done

Sebuah feature selesai bila:

- Domain rule sudah diterapkan.
- TUI state mencakup loading, success, empty, error, dan cancellation.
- Unit dan integration test tersedia.
- Workflow retry, stale recovery, dan idempotency dipertimbangkan.
- Tool atau command baru memiliki security policy.
- Durable activity event dan diagnostic log tersedia.
- Dokumentasi protocol, schema, dan config diperbarui.
- Tidak ada secret atau host path yang bocor ke log.
