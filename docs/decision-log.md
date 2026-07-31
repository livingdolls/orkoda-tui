# Architecture Decision Log

## ADR-001 — OpenTUI as Primary User Interface

**Status:** Accepted

Gunakan OpenTUI dengan TypeScript sebagai interface utama. Tidak ada Next.js atau browser frontend pada product scope awal.

---

## ADR-002 — Product Scope Is Software Development Only

**Status:** Accepted

Platform hanya menangani planning, implementation, checks, review, approval, dan Git publication untuk source code repository.

---

## ADR-003 — Golang for the Local Daemon

**Status:** Accepted

Golang digunakan untuk protocol, state machine, workspace management, scheduler, agent workers, sandbox orchestration, Git adapters, dan persistence access.

---

## ADR-004 — SQLite as Source of Truth

**Status:** Accepted

Workflow state, repository metadata, plan, execution, tool run, check, review, approval, publication, queue state, dan activity timeline disimpan di SQLite.

### Consequences

- Tidak ada PostgreSQL service atau database credential.
- Database dapat didistribusikan sebagai satu file lokal.
- WAL, foreign key, busy timeout, migration, backup, dan stale-job recovery harus ditangani application layer.
- Transaction harus singkat karena SQLite hanya memiliki satu writer pada satu waktu.

---

## ADR-005 — SQLite-Backed Durable Queue

**Status:** Accepted

RabbitMQ dan transactional outbox tidak digunakan. Durable command disimpan di tabel `jobs` dan diklaim secara atomik menggunakan SQLite `UPDATE ... RETURNING`.

### Consequences

- Retry, backoff, attempts, dead status, cancellation, dan stale lock recovery diimplementasikan dalam domain queue.
- Handler wajib idempotent.
- Queue hanya untuk satu local daemon dan tidak dirancang sebagai distributed broker.

---

## ADR-006 — Personal Local-Only Runtime

**Status:** Accepted

Orkoda berjalan pada satu komputer developer. OpenTUI berkomunikasi dengan local daemon melalui localhost HTTP atau Unix socket. Remote API, multi-user authentication, tenant, server deployment, dan distributed worker berada di luar MVP.

### Consequences

- Tidak ada Redis, service discovery, remote session, atau centralized credential store.
- Cache non-kritis berada di process memory.
- Event fan-out menggunakan in-memory event bus.
- Docker hanya digunakan untuk sandbox command bila diperlukan.

---

## ADR-007 — Isolated Workspace Is Mandatory

**Status:** Accepted

Executor tidak pernah menulis langsung ke source repository. Setiap job menggunakan Git worktree atau isolated clone yang terikat pada base commit SHA.

---

## ADR-008 — Repository Tools Are Core MVP

**Status:** Accepted

File read/search/patch, Git diff, sandboxed command runner, dan automated checks merupakan bagian MVP karena produk khusus software development.

---

## ADR-009 — Separate Executor and Reviewer Roles

**Status:** Accepted

Executor dan Reviewer menggunakan agent configuration dan role terpisah. Mereka dapat berjalan sebagai goroutine dalam daemon yang sama, tetapi Reviewer tetap read-only pada MVP.

---

## ADR-010 — Human Approval Is Mandatory Before Publication

**Status:** Accepted

Tidak ada commit, push, pull request, merge, atau deployment yang dipublish hanya berdasarkan keputusan agent.

---

## ADR-011 — Approval Binds to Immutable Diff

**Status:** Accepted

Approval menyimpan execution version, base commit SHA, dan patch checksum. Perubahan diff setelah approval membatalkan approval.

---

## ADR-012 — Durable Timeline with In-Memory Live Events

**Status:** Accepted

Event permanen disimpan di tabel `activity_events`. Setelah commit, event dipublish ke in-memory subscriber untuk update OpenTUI. Reconnect membaca sequence terakhir dari SQLite.

---

## ADR-013 — Provider-Agnostic LLM Gateway

**Status:** Accepted

Domain tidak memanggil provider SDK secara langsung. Provider adapter menormalisasi request, response, error, usage, timeout, dan cancellation.

---

## ADR-014 — Network Disabled by Default in Sandbox

**Status:** Accepted

Command agent tidak memiliki network access kecuali project policy memberikan scope eksplisit untuk registry atau host tertentu.

---

## ADR-015 — OpenTUI React Renderer and Bun Runtime

**Status:** Accepted

Gunakan `@opentui/react` dan Bun untuk shell terminal. React renderer dipilih agar navigation, state, focus, modal, toast, log, dan diff component dapat disusun secara deklaratif.

---

## ADR-016 — Local Filesystem Artifact Storage

**Status:** Accepted

Patch, command log, test report, dan debug bundle disimpan di bawah `.orkoda/artifacts`. Write dilakukan melalui temporary file dan atomic rename; path traversal keluar dari root storage ditolak.
