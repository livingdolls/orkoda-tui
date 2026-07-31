# Architecture Decision Log

## ADR-001 — OpenTUI as Primary User Interface

**Status:** Accepted

### Decision

Gunakan OpenTUI dengan TypeScript sebagai interface utama. Tidak ada Next.js atau browser frontend pada product scope awal.

### Consequences

- Workflow dapat digunakan langsung dari terminal developer.
- UI harus menangani keyboard, focus, resize, streaming log, dan diff dengan baik.
- Dibutuhkan protocol versioning antara TUI dan Golang API/local daemon.

---

## ADR-002 — Product Scope Is Software Development Only

**Status:** Accepted

### Decision

Platform hanya menangani planning, implementation, checks, review, approval, dan Git publication untuk source code repository.

### Consequences

- Agent prompts, database, metrics, dan UI dapat dioptimalkan untuk repository, diff, test, dan Git.
- Use case content writing, generic document analysis, dan non-software project planning dikeluarkan.

---

## ADR-003 — Golang for API, Orchestrator, and Workers

**Status:** Accepted

Golang digunakan untuk state machine, queue consumers, workspace management, sandbox orchestration, Git adapters, dan backend protocol.

---

## ADR-004 — PostgreSQL as Source of Truth

**Status:** Accepted

Workflow state, base commit, workspace metadata, execution, tool run, check, review, approval, publication, dan audit disimpan di PostgreSQL. Redis dan RabbitMQ bukan sumber kebenaran bisnis.

---

## ADR-005 — RabbitMQ and Transactional Outbox

**Status:** Accepted

Gunakan RabbitMQ untuk durable work queue dan transactional outbox untuk menghindari dual-write inconsistency. Semua consumer idempotent.

---

## ADR-006 — Local and Remote Modes Share One Protocol

**Status:** Accepted

OpenTUI berkomunikasi dengan local daemon melalui Unix socket/localhost dan dengan remote API melalui HTTPS. Keduanya menggunakan versioned command, query, dan event schemas yang sama.

---

## ADR-007 — Isolated Workspace Is Mandatory

**Status:** Accepted

Executor tidak pernah menulis langsung ke source repository. Setiap job menggunakan Git worktree atau isolated clone yang terikat pada base commit SHA.

### Consequences

- Source repository tetap aman dan mudah dipulihkan.
- Diperlukan workspace lifecycle, lease, quota, cleanup, dan patch recovery.

---

## ADR-008 — Repository Tools Are Core MVP

**Status:** Accepted

File read/search/patch, Git diff, sandboxed command runner, dan automated checks merupakan bagian MVP karena produk khusus software development.

### Consequences

- MVP lebih kompleks daripada text-only workflow.
- Security hardening sandbox harus dilakukan sebelum release.

---

## ADR-009 — Separate Executor and Reviewer

**Status:** Accepted

Executor dan Reviewer menggunakan agent configuration dan role terpisah. Reviewer bersifat read-only pada MVP.

---

## ADR-010 — Human Approval Is Mandatory Before Publication

**Status:** Accepted

Tidak ada commit, push, pull request, merge, atau deployment yang dipublish hanya berdasarkan keputusan agent.

---

## ADR-011 — Approval Binds to Immutable Diff

**Status:** Accepted

Approval menyimpan execution version, base commit SHA, dan patch checksum. Perubahan diff setelah approval membatalkan approval.

---

## ADR-012 — Event Stream Instead of Browser-Specific Realtime

**Status:** Accepted

TUI menerima event melalui local IPC stream atau HTTP event stream. Event permanen tetap tersimpan di database dan dapat di-replay berdasarkan sequence.

---

## ADR-013 — Provider-Agnostic LLM Gateway

**Status:** Accepted

Domain tidak memanggil provider SDK secara langsung. Provider adapter menormalisasi request, response, error, usage, timeout, dan cancellation.

---

## ADR-014 — Network Disabled by Default in Sandbox

**Status:** Accepted

Command agent tidak memiliki network access kecuali project policy memberikan scope eksplisit untuk registry atau host tertentu.
