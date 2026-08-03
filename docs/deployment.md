# Distribution and Local Operations

## 1. Supported Mode

Orkoda hanya mendukung personal local mode pada MVP:

```text
OpenTUI client
Local Go daemon
SQLite database
Local artifacts and workspaces
Optional Docker command sandbox
```

Tidak ada remote API, hosted control plane, external database, cache server, message broker, atau object storage.

## 2. Requirements

- Go 1.26 atau lebih baru.
- Bun 1.3.14 atau lebih baru.
- Git.
- Docker opsional untuk sandbox command pada fase executor.

## 3. Local Setup

```bash
cp .env.example .env
make install
make migrate
```

Jalankan daemon:

```bash
make api
```

Jalankan OpenTUI pada terminal lain:

```bash
make tui
```

Daemon menjalankan migration idempotent pada startup, sehingga `make migrate` bersifat eksplisit tetapi tidak wajib setiap kali aplikasi dijalankan.

## 4. Runtime Files

```text
.orkoda/
├── orkoda.db
├── orkoda.db-wal
├── orkoda.db-shm
├── artifacts/
├── workspaces/
├── api.token
├── orkoda.db.bak
└── logs/
```

`.orkoda/` harus berada pada filesystem lokal. Jangan meletakkan database WAL aktif pada network filesystem yang tidak menjamin locking SQLite.

## 5. Build Artifacts

- Go local-daemon binary.
- OpenTUI package atau bundled executable.
- Versioned JSON Schema dan generated protocol types.
- Optional sandbox base image.

Tidak ada API image, worker image, PostgreSQL, Redis, RabbitMQ, atau MinIO image.

## 6. Health Endpoints

```text
GET /health/live
GET /api/v1/status
```

Readiness endpoint dapat ditambahkan ketika repository, provider, dan sandbox dependency sudah tersedia. Pemeriksaan dependency tidak boleh melakukan operasi mahal.

## 7. Graceful Shutdown

1. Batalkan root context daemon.
2. Hentikan penerimaan API request baru.
3. Hentikan queue polling dan scheduler.
4. Tunggu handler yang aman untuk diselesaikan.
5. Simpan checkpoint workspace bila diperlukan.
6. Kembalikan job yang tidak selesai ke status recoverable.
7. Tutup subscriber event bus.
8. Tutup SQLite connection.

## 8. SQLite Operations

### Migration

- Migration berjalan otomatis saat daemon startup.
- Jalankan `make migrate` untuk initialization atau pemeriksaan manual.
- Migration harus idempotent dan teruji pada database temporary.

### Backup

Sebelum migration startup, daemon melakukan WAL checkpoint lalu membuat backup atomik `orkoda.db.bak` dengan permission `0600`. Jangan menyalin database aktif secara manual tanpa checkpoint.

### Recovery

- Jalankan SQLite integrity check pada database yang diduga rusak.
- Pulihkan database dan folder artifact sebagai satu set.
- Workspace dapat dibuat ulang dari base commit dan patch artifact.
- Job `RUNNING` yang stale dikembalikan ke queue pada startup.

## 9. CI Pipeline

1. Go format check.
2. Go vet dan unit test.
3. SQLite migration dan queue integration test menggunakan temporary directory.
4. TypeScript lint dan type check.
5. OpenTUI tests.
6. JSON Schema compatibility test.
7. Dependency dan secret scan.
8. Build daemon dan TUI artifact.
9. Build optional Docker sandbox image dan jalankan sandbox security suite ketika Docker tersedia.

CI tidak menjalankan service container.

## 10. Configuration

Configuration precedence:

```text
Built-in defaults
-> environment variables
-> command-line flags (future)
```

Konfigurasi utama:

```text
ORKODA_DATA_DIR
ORKODA_DATABASE_PATH
ORKODA_ARTIFACT_DIR
ORKODA_API_HOST
ORKODA_API_PORT
ORKODA_API_TOKEN
ORKODA_API_TOKEN_FILE
ORKODA_SANDBOX_MODE=docker
ORKODA_SANDBOX_IMAGE=orkoda-sandbox:local
ORKODA_ALLOW_UNSANDBOXED_CHECKS=false
ORKODA_SHUTDOWN_TIMEOUT
```

Credential provider dan Git disimpan melalui OS keychain adapter bila tersedia. Credential tidak boleh disimpan di SQLite dalam plaintext.

## 11. Reset Local State

```bash
make clean-data
```

Command tersebut menghapus seluruh `.orkoda/`, termasuk database, approval history, artifact, dan workspace. Gunakan hanya ketika reset total memang diinginkan.

## 12. Future Scope Boundary

Arsitektur distributed hanya dipertimbangkan bila Orkoda berubah menjadi multi-machine. Pada kondisi tersebut, keputusan baru harus dibuat untuk database, broker, event distribution, storage, authentication, dan deployment. Komponen itu tidak disiapkan secara prematur pada MVP lokal.
