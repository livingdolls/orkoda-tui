# Database Design

## 1. Prinsip

- SQLite adalah satu-satunya source of truth.
- Database berada di `.orkoda/orkoda.db` secara default.
- Seluruh timestamp disimpan sebagai Unix milliseconds dalam kolom `INTEGER`.
- ID domain menggunakan string acak agar tidak bergantung pada extension database.
- JSON disimpan sebagai `TEXT` dan divalidasi application layer.
- Foreign key diaktifkan pada setiap koneksi.
- Transaction harus pendek; model call, command, dan Git operation berjalan di luar transaction.
- File besar disimpan pada local artifact storage, bukan sebagai database BLOB.

## 2. SQLite Runtime Configuration

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA synchronous = NORMAL;
```

Daemon menggunakan satu pooled database connection untuk menjaga pragma per-connection konsisten dan menghindari competing writer di dalam satu process.

## 3. Foundation Tables

### Durable jobs

```sql
CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL
        CHECK (status IN ('QUEUED', 'RUNNING', 'COMPLETED', 'DEAD')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
    run_after INTEGER NOT NULL,
    locked_by TEXT,
    locked_at INTEGER,
    last_error TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX idx_jobs_claim
    ON jobs(status, run_after, created_at);
```

Queue claim dilakukan dalam satu statement:

```sql
UPDATE jobs
SET status = 'RUNNING',
    attempts = attempts + 1,
    locked_by = ?,
    locked_at = ?,
    updated_at = ?
WHERE id = (
    SELECT id
    FROM jobs
    WHERE status = 'QUEUED' AND run_after <= ?
    ORDER BY run_after, created_at
    LIMIT 1
)
RETURNING *;
```

### Activity events

```sql
CREATE TABLE activity_events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT,
    type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_activity_events_job_sequence
    ON activity_events(job_id, sequence);
```

`activity_events` adalah durable timeline. In-memory event bus hanya mengirim notifikasi live setelah transaction event berhasil commit.

## 4. Planned Domain Tables

Schema berikut ditambahkan secara bertahap:

- `projects` dan `repositories`;
- `plans` dan `plan_versions`;
- `agent_configs` dan `tool_policies`;
- `workflow_jobs` dan `workspaces`;
- `executions` dan `tool_runs`;
- `check_definitions` dan `check_runs`;
- `reviews` dan `review_issues`;
- `revision_requests` dan `approvals`;
- `git_publications` dan `artifacts` metadata.

Karena produk bersifat personal-local, tabel user, membership, session, refresh token, tenant, dan organization tidak termasuk MVP.

## 5. Workflow Concurrency

- Satu daemon lokal memproses queue.
- Atomic claim mencegah job yang sama dijalankan dua goroutine.
- `attempts` bertambah pada saat claim.
- Failed job kembali ke `QUEUED` dengan `run_after` baru hingga mencapai `max_attempts`.
- Setelah batas tercapai, status menjadi `DEAD` dan membutuhkan retry manual.
- Job `RUNNING` dengan `locked_at` stale dikembalikan ke `QUEUED` saat startup.
- Handler tetap wajib idempotent karena daemon dapat berhenti setelah side effect tetapi sebelum status selesai tersimpan.

## 6. Workspace Lease

Workspace lease nantinya disimpan sebagai:

```sql
CREATE TABLE workspace_leases (
    workspace_id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    acquired_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
```

Lease tidak menggantikan filesystem permission dan path guard. Source repository tetap read-only terhadap executor.

## 7. Backup and Recovery

Backup source database dibuat setelah WAL checkpoint dan dipublikasikan atomik
sebagai `.orkoda/orkoda.db.bak` dengan permission `0600`. Backup lokal mencakup:

```text
.orkoda/orkoda.db
.orkoda/orkoda.db-wal
.orkoda/orkoda.db-shm
.orkoda/artifacts/
```

Untuk backup konsisten saat daemon aktif, gunakan SQLite backup API atau jalankan checkpoint sebelum menyalin database. Menyalin hanya file `.db` ketika WAL aktif tanpa checkpoint dapat menghasilkan backup yang tidak lengkap.

Workspace dapat direkonstruksi dari repository base commit dan patch artifact. Database, artifact, approval, dan publication record tidak boleh terhapus oleh cleanup workspace.

## 8. Migration Rules

- Migration bersifat idempotent dan berjalan otomatis saat daemon startup.
- `make migrate` tersedia untuk menjalankan migration secara eksplisit.
- Perubahan destructive menggunakan create-copy-rename, bukan mengandalkan unsupported `ALTER TABLE` behavior.
- Sebelum migration besar, buat backup database lokal.
- CI menjalankan migration pada database temporary dan memverifikasi schema utama.
