# Deployment and Operations

## 1. Distribution Modes

### Local Mode

- OpenTUI TypeScript application.
- Golang local daemon.
- Local PostgreSQL atau embedded development profile untuk prototype.
- Docker-based sandbox.
- Repository dan workspace pada mesin developer.

### Remote Mode

- OpenTUI tetap berjalan di terminal developer.
- API, database, Redis, RabbitMQ, workers, sandbox pool, dan storage berjalan di server.
- Repository remote dikloning ke ephemeral workspace.

## 2. Local Development

```text
OpenTUI app
Golang API/local daemon
Executor worker
Reviewer worker
PostgreSQL
Redis
RabbitMQ
MinIO
Docker sandbox runtime
```

Docker Compose digunakan untuk dependency. TUI dijalankan langsung agar iterasi UI cepat.

## 3. Build Artifacts

- TUI package atau bundled executable untuk platform yang didukung.
- Golang binaries: `api`, `worker`, dan `migrate`.
- Container images untuk API, workers, and sandbox base images.
- Versioned JSON Schema dan protocol types.

TUI dan API harus melakukan protocol-version negotiation.

## 4. Production Components

- API replicas.
- Outbox publisher.
- Executor worker pool.
- Check runner pool.
- Reviewer worker pool.
- Publication worker.
- PostgreSQL.
- Redis.
- RabbitMQ.
- Object storage.
- Sandbox runtime nodes.
- Metrics, logs, and tracing backend.

Tidak ada Next.js atau browser frontend dalam deployment.

## 5. Container and Sandbox Requirements

Service container:

- Non-root.
- Read-only filesystem bila memungkinkan.
- Health endpoint.
- Graceful shutdown.
- Resource requests and limits.

Sandbox container lebih ketat:

- No privileged mode.
- No Docker socket.
- Network disabled by default.
- Temporary filesystem.
- Explicit workspace mount.
- PID and output limits.

## 6. Health Endpoints

```text
GET /health/live
GET /health/ready
GET /health/dependencies
```

Readiness memeriksa database dan dependency wajib tanpa melakukan operasi mahal.

## 7. Graceful Shutdown

Worker:

1. Stop consuming messages.
2. Mark lease as draining.
3. Batalkan atau selesaikan tool sesuai policy.
4. Simpan checkpoint.
5. Ack hanya setelah durable state tersimpan.
6. Lepaskan workspace lease.

## 8. Database Migration

- Migration backward-compatible.
- Expand/migrate/contract untuk perubahan besar.
- Migration tidak dijalankan serentak oleh seluruh replica.
- Backup dan restore test dilakukan sebelum release penting.

## 9. CI/CD Pipeline

1. Go format, lint, test, and race test.
2. TypeScript type check and lint.
3. OpenTUI component tests.
4. Integration tests untuk database, queue, Git, dan provider fixtures.
5. JSON Schema compatibility test.
6. Dependency and secret scan.
7. Build Go binaries dan TUI artifact.
8. Build/sign container images.
9. Run sandbox security suite.
10. Deploy staging dan run end-to-end workflow.
11. Manual approval untuk production backend release.

## 10. Configuration

Configuration precedence:

```text
Built-in defaults
-> config file
-> environment variables
-> command-line flags
```

TUI config menyimpan endpoint, profile, key binding, dan display preference. Secret tetap berada di keychain atau secret manager.

## 11. Scaling

- Scale Executor berdasarkan queue depth dan provider quota.
- Scale Reviewer terpisah.
- Check runner dapat dikelompokkan berdasarkan language/toolchain image.
- Sandbox node dipisahkan dari API node.
- Workspace storage quota dipantau per node dan project.

## 12. Backup and Recovery

Backup:

- PostgreSQL.
- Artifact storage.
- Provider/Git credential metadata terenkripsi.
- Configuration dan schema versions.

Recovery job dapat membuat ulang workspace dari repository base commit dan patch artifact.

## 13. Rollback

- TUI dapat mendukung satu atau lebih protocol version sebelumnya.
- Backend rollback hanya dilakukan bila database migration kompatibel.
- Sandbox image version dicatat per execution.
- Feature flag dapat menonaktifkan provider, tool, command profile, atau Git publication tanpa menghentikan seluruh sistem.
