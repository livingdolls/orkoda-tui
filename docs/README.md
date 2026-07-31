# AI Software Development Agent TUI

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

Pengguna memberikan requirement atau planning. Sistem menyiapkan workspace repository yang terisolasi, Executor mengubah source code, automated checks menjalankan test/linter/build, Reviewer memeriksa diff dan evidence, lalu pengguna menyetujui, meminta revisi, atau menolak hasil melalui OpenTUI.

## Scope Produk

Produk ini khusus untuk software development, termasuk:

- Implementasi feature.
- Bug fixing.
- Refactoring.
- Penambahan dan perbaikan test.
- Dependency update.
- Pembuatan migration dan API.
- Dokumentasi teknis yang berada di repository.
- Code review dan security review.
- Pembuatan commit atau draft pull request setelah approval.

Produk tidak ditujukan untuk penulisan konten umum, marketing workflow, analisis dokumen umum, atau project management non-software.

## Stack Utama

| Area | Teknologi |
|---|---|
| Terminal UI | OpenTUI, TypeScript, React renderer |
| Backend API dan Orchestrator | Golang, Gin, pgx |
| Database | PostgreSQL |
| Cache, lock, dan event fan-out | Redis |
| Durable job queue | RabbitMQ |
| Workspace | Git worktree atau isolated clone |
| Sandbox | Docker container dengan resource limit |
| Artifact storage | Local storage untuk mode lokal; S3/GCS untuk mode server |
| AI provider | OpenAI, Anthropic, Gemini, DeepSeek, OpenRouter, atau Ollama |
| Observability | OpenTelemetry, Prometheus-compatible metrics, structured logs |

## Mode Operasi

### Local Mode

OpenTUI, local daemon, workspace manager, dan sandbox berjalan di komputer developer. Cocok untuk penggunaan personal dan repository lokal.

### Remote Mode

OpenTUI menjadi client untuk API dan worker yang berjalan di server. Repository diproses dalam workspace sandbox di server dan hasil dikirim sebagai diff atau pull request.

## MVP

MVP harus mendukung:

1. Membuka atau menghubungkan repository Git.
2. Menulis requirement dan acceptance criteria dari terminal.
3. Menormalisasi requirement menjadi implementation plan.
4. Menyiapkan branch dan workspace terisolasi.
5. Executor membaca dan mengubah file repository.
6. Executor menjalankan command yang diizinkan.
7. Sistem menjalankan test, linter, type check, dan build yang dikonfigurasi.
8. Reviewer memeriksa plan, diff, hasil check, dan risiko.
9. User melihat diff dan review di OpenTUI.
10. User memilih approve, request revision, atau reject.
11. Setelah approval, sistem membuat commit atau draft pull request.
12. Seluruh execution, command, review, revisi, dan approval tersimpan.

## Isi Dokumentasi

- [`spec.md`](./spec.md): product specification khusus software development.
- [`plan.md`](./plan.md): strategi implementasi.
- [`architecture.md`](./architecture.md): arsitektur OpenTUI, backend, worker, workspace, dan sandbox.
- [`workflow.md`](./workflow.md): state machine dari requirement hingga commit/PR.
- [`database.md`](./database.md): schema repository, workspace, execution, tool run, review, dan approval.
- [`api.md`](./api.md): API internal untuk OpenTUI dan worker.
- [`agent-contract.md`](./agent-contract.md): kontrak Planning Agent, Executor, dan Reviewer.
- [`security.md`](./security.md): keamanan repository, command, secret, sandbox, dan Git publishing.
- [`testing.md`](./testing.md): strategi test untuk TUI, workflow, Git, dan sandbox.
- [`observability.md`](./observability.md): logging, metrics, tracing, timeline, dan debug bundle.
- [`deployment.md`](./deployment.md): local mode, remote mode, CI/CD, dan operasi.
- [`roadmap.md`](./roadmap.md): fase pengembangan produk.
- [`task.md`](./task.md): backlog implementasi terurut.
- [`decision-log.md`](./decision-log.md): architecture decision records.

## Urutan Implementasi

1. Finalisasi keputusan di `decision-log.md`.
2. Bangun OpenTUI shell dan local API contract.
3. Implementasikan repository discovery dan isolated workspace.
4. Implementasikan state machine dan durable queue.
5. Implementasikan Executor dengan file dan command tools.
6. Tambahkan automated checks dan Reviewer.
7. Tambahkan approval, revision loop, commit, dan pull request.
8. Terapkan hardening sandbox sebelum penggunaan repository penting.
