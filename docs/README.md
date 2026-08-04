# Orkoda — AI Software Development Agent TUI

Platform terminal-native untuk mengorkestrasi pekerjaan software development dengan alur:

```text
Developer Requirement
        ↓
Planning Agent
        ↓
Executor Agent
        ↓
Automated Checks
        ↓
Reviewer Agent
        ↓
Human Approval
        ↓
Commit / Pull Request
```

Orkoda ditujukan untuk penggunaan pribadi pada satu komputer. OpenTUI menjadi interface utama dan satu Golang daemon menjalankan workflow, scheduler, serta agent workers secara lokal.

Interface utama menggunakan Unified Kanban Workspace. Satu kartu mengikuti pekerjaan yang sama dari planning sampai publication sehingga pengguna tidak perlu berpindah antara layar Projects dan Jobs.

## Scope Produk

- Implementasi feature dan bug fixing.
- Refactoring dan dependency update.
- Penambahan atau perbaikan test.
- Migration, API, dan dokumentasi teknis repository.
- Code review dan security review.
- Commit atau draft pull request setelah human approval.

Produk tidak ditujukan sebagai SaaS, hosted platform, multi-user system, atau generic project management tool.

## Stack Utama

| Area | Teknologi |
|---|---|
| Terminal UI | OpenTUI React, TypeScript, Bun |
| Local daemon | Golang, Gin |
| Source of truth | Embedded SQLite dengan WAL |
| Durable jobs | SQLite-backed queue |
| Live events | In-memory Go event bus |
| Cache dan local lock | Process memory, `sync`, OS file lock, SQLite lease |
| Workspace | Local Git worktree |
| Sandbox | Docker container, optional dan network disabled by default |
| Artifact storage | Local filesystem di `.orkoda/artifacts` |
| AI provider | Provider-agnostic gateway |
| Observability | Structured local logs dan diagnostic bundle |

PostgreSQL, Redis, RabbitMQ, MinIO, remote worker, dan server deployment tidak termasuk scope MVP.

## Local Runtime

```text
OpenTUI
   │
   ▼
Orkoda local daemon
├── HTTP / Unix socket protocol
├── workflow state machine
├── SQLite scheduler and queue
├── planning/executor/reviewer workers
├── in-memory live event bus
└── workspace and artifact managers
   │
   ├── .orkoda/orkoda.db
   ├── .orkoda/artifacts/
   └── .orkoda/workspaces/
```

## MVP

1. Membuka repository Git lokal.
2. Menulis requirement dan acceptance criteria.
3. Menormalisasi requirement menjadi implementation plan.
4. Menyiapkan Git worktree terisolasi.
5. Executor membaca dan mengubah file workspace.
6. Executor menjalankan command yang diizinkan dalam sandbox.
7. Sistem menjalankan test, linter, type check, dan build.
8. Reviewer memeriksa plan, diff, checks, dan risiko.
9. User melihat progress, diff, checks, dan review pada kartu Board yang sama.
10. User memilih approve, request revision, atau reject dari detail kartu.
11. Setelah approval, sistem membuat commit atau optional draft pull request GitHub.
12. Execution, command, review, revision, dan approval tersimpan di SQLite.

## Isi Dokumentasi

- [`spec.md`](./spec.md): product specification.
- [`plan.md`](./plan.md): strategi implementasi local-only.
- [`architecture.md`](./architecture.md): arsitektur daemon, SQLite, queue, workspace, dan sandbox.
- [`workflow.md`](./workflow.md): state machine workflow.
- [`kanban-board.md`](./kanban-board.md): Unified Kanban Workspace, status mapping, navigation, data loading, dan approval UX.
- [`database.md`](./database.md): SQLite schema dan persistence rules.
- [`api.md`](./api.md): protocol antara OpenTUI dan daemon.
- [`agent-contract.md`](./agent-contract.md): kontrak Planning Agent, Executor, dan Reviewer.
- [`security.md`](./security.md): repository, secret, command, sandbox, dan publication security.
- [`testing.md`](./testing.md): strategi test.
- [`observability.md`](./observability.md): logging, timeline, metrics, dan debug bundle.
- [`deployment.md`](./deployment.md): local distribution dan operasi.
- [`roadmap.md`](./roadmap.md): fase pengembangan.
- [`task.md`](./task.md): backlog implementasi.
- [`decision-log.md`](./decision-log.md): architecture decisions.

## Urutan Implementasi

1. Foundation OpenTUI, daemon, SQLite, artifact store, dan job queue.
2. Repository discovery dan isolated worktree.
3. Workflow state machine dan Planning Agent.
4. Executor tools dan Docker sandbox.
5. Automated checks dan Reviewer.
6. Approval, revision loop, commit, dan pull request.
7. Local reliability, security, diagnostics, dan distribution.
8. Unified Kanban Workspace untuk menyatukan seluruh user journey.
