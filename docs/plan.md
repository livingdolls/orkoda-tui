# Implementation Plan

## 1. Prinsip Implementasi

- Produk hanya melayani workflow software development.
- OpenTUI adalah interface utama; domain tidak bergantung pada UI.
- Repository tidak pernah diedit langsung tanpa isolated workspace.
- State machine, audit trail, dan immutable execution version dibuat sejak awal.
- File write dan command execution hanya aktif setelah sandbox policy tersedia.
- Executor, automated checks, Reviewer, dan Human Approval merupakan tahap terpisah.
- Publish ke Git hanya dilakukan dari execution version yang approved.

## 2. Strategi Delivery

1. Foundation dan OpenTUI shell.
2. Repository dan workspace manager.
3. Workflow state machine dan durable jobs.
4. LLM gateway dan Planning Agent.
5. Executor tools dan sandbox.
6. Automated checks dan Reviewer.
7. Human approval, revision, commit, dan pull request.
8. Reliability, security, observability, dan distribution.

## 3. Phase 1 — Foundation and OpenTUI

### Hasil

- TUI dapat dijalankan dan terhubung ke local daemon.
- Project/repository dapat didaftarkan.
- Navigation, command palette, config, dan logging tersedia.

### Pekerjaan

- Setup TypeScript workspace untuk `@opentui/core` atau `@opentui/react`.
- Buat app shell, keyboard map, focus management, modal, toast, dan error boundary.
- Setup Golang API/local daemon.
- Setup PostgreSQL, Redis, RabbitMQ, dan migration.
- Definisikan protocol antara TUI dan API.
- Implementasikan token/profile storage yang aman untuk remote mode.
- Buat repository list, project detail, settings, dan job list screens.

## 4. Phase 2 — Repository and Workspace

### Hasil

- Repository dapat diinspeksi tanpa diubah.
- Setiap job memperoleh workspace terisolasi dari base commit tertentu.

### Pekerjaan

- Repository discovery dan validation.
- Git remote, branch, commit, dirty state, dan submodule detection.
- Git worktree adapter untuk local mode.
- Isolated clone adapter untuk remote mode.
- Workspace lifecycle: prepare, ready, locked, archived, cleanup.
- File index dan ignore policy.
- Patch snapshot dan workspace recovery.
- Restricted mode untuk repository yang belum dipercaya.

## 5. Phase 3 — Workflow and LLM Gateway

### Hasil

- Requirement dapat dinormalisasi dan job dapat berjalan melalui durable queue.

### Pekerjaan

- Implementasikan Job aggregate dan transition table.
- Implementasikan transactional outbox dan idempotent consumer.
- Definisikan canonical LLM request/response.
- Implementasikan provider adapter pertama dan fake provider.
- Buat Planning Agent untuk menghasilkan affected areas, steps, tests, dan criteria.
- Tambahkan token/cost budget, cancellation, retry, dan timeout.
- Stream event job ke TUI melalui local IPC atau HTTP event stream.

## 6. Phase 4 — Executor and Tool Runtime

### Hasil

- Executor dapat melakukan perubahan source code yang dapat diaudit.

### Pekerjaan

- File read, glob, search, patch, create, dan delete tools.
- Git status dan diff tools.
- Sandboxed command runner.
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

- User dapat mengontrol hasil akhir dari terminal.

### Pekerjaan

- Diff viewer per file dan hunk.
- Review issue panel dan acceptance criteria matrix.
- Approve, approve with notes, request revision, reject, dan take over actions.
- Bind approval ke execution version, base SHA, dan diff checksum.
- Revision context builder.
- Commit message generator dan local commit.
- Git provider adapter pertama.
- Push branch dan create draft pull request.
- Publication idempotency dan conflict detection.

## 9. Phase 7 — Production Readiness

### Reliability

- Graceful shutdown.
- Worker recovery.
- Queue retry dan DLQ.
- Workspace orphan recovery.
- Database backup dan restore drill.

### Security

- Secret scanning sebelum model input dan publication.
- Symlink/path traversal defense.
- Sandbox hardening.
- Signed Git provider credentials.
- Repository trust policy.

### Observability

- Structured logs dan correlation IDs.
- OpenTelemetry traces.
- Workflow, queue, provider, sandbox, dan Git metrics.
- TUI diagnostics screen dan exportable debug bundle.

## 10. Struktur Repository

```text
ai-dev-agent/
├── apps/
│   └── tui/                    # OpenTUI + TypeScript
├── cmd/
│   ├── api/                    # Golang API / local daemon
│   ├── worker/                 # Executor and reviewer workers
│   └── migrate/
├── internal/
│   ├── identity/
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
│   ├── protocol/               # Shared JSON schema / generated types
│   └── prompts/
├── migrations/
├── deployments/
├── docs/
├── docker-compose.yml
├── Makefile
└── README.md
```

## 11. Definition of Done

Sebuah feature selesai bila:

- Domain rule dan authorization sudah diterapkan.
- TUI state mencakup loading, success, empty, error, dan cancellation.
- Unit dan integration test tersedia.
- Workflow retry dan idempotency dipertimbangkan.
- Tool atau command baru memiliki security policy.
- Audit event dan metrics tersedia.
- Dokumentasi protocol dan config diperbarui.
- Tidak ada secret atau host path yang bocor ke log.
