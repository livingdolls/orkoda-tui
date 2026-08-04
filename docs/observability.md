# Observability

## 1. Tujuan

- Mengetahui posisi setiap job.
- Menelusuri model call, tool run, check, review, approval, dan publication.
- Mendeteksi stuck worker, orphan workspace, sandbox violation, dan Git failure.
- Menyediakan diagnostics yang dapat dibaca dari OpenTUI.

## 2. Structured Logging

Field minimum:

```text
timestamp
level
service
request_id
correlation_id
user_id
project_id
repository_id
job_id
workspace_id
execution_id
tool_run_id
check_run_id
review_id
publication_id
event
error_code
duration_ms
```

Jangan log:

- Full prompt secara default.
- Full source code.
- Environment variables.
- Provider key, Git token, cookie, atau SSH key.
- Unredacted command output yang terdeteksi mengandung secret.

## 3. Metrics

### API and TUI Protocol

- Request count, latency, and errors.
- Active event streams.
- Reconnect count.
- TUI command failure rate.

### Queue and Workers

- Queue depth and oldest message age.
- Processing duration.
- Retry and DLQ count.
- Active workers and heartbeat age.

### Workflow

- Jobs by status.
- Time per stage.
- Revision count.
- Approval wait time.
- Completion and rejection rate.

### Tools and Sandbox

- Tool calls by type and status.
- Command duration and timeout count.
- Policy denial count.
- Sandbox startup failure.
- CPU, memory, disk, and output-limit violation.

### AI Provider

- Request count and latency.
- Input/output tokens.
- Estimated cost.
- Rate limit, timeout, and schema failure.

### Git

- Workspace preparation time.
- Commit, push, and PR success rate.
- Base branch conflict.
- Publication retry count.

## 4. Tracing

Trace utama:

```text
TUI command
  -> API command
  -> database transaction
  -> outbox publish
  -> worker consume
  -> model call
  -> tool/sandbox span
  -> checks
  -> reviewer
  -> approval/publication
```

Source code content tidak ditempatkan dalam span attribute.

## 5. Product Timeline vs Operational Log

### Product Timeline

Ditampilkan di OpenTUI:

- Workspace prepared.
- Executor started.
- File changed.
- Command completed.
- Check passed/failed.
- Review completed.
- Approval requested.
- Revision requested.
- Commit or PR created.

### Operational Log

Untuk operator:

- SQL error.
- Queue redelivery.
- Provider failure.
- Sandbox runtime failure.
- Git credential error.
- Outbox backlog.

## 6. Alerts

- DLQ message > 0.
- Oldest queue message melewati threshold.
- Job tidak memiliki event dalam batas waktu stage.
- Workspace lease expired tetapi masih marked active.
- Sandbox violation meningkat.
- Provider error atau cost spike.
- Publication failure berulang.
- Disk workspace mendekati quota.

## 7. OpenTUI Diagnostics

Diagnostics screen menampilkan:

- Client and daemon version.
- Connection status.
- Active profile and endpoint.
- Worker and queue health summary.
- Current job correlation ID.
- Workspace path/reference.
- Last errors.

User dapat mengekspor debug bundle yang sudah di-redact berisi config non-secret, version, logs terbatas, event timeline, dan health response.

Daemon menyediakan `GET /api/v1/metrics` untuk counter process-local (request,
error, latency total, active stream, reconnect, retry/dead queue, dan policy
denial), `GET /api/v1/diagnostics` untuk health SQLite/queue/workspace, serta
`POST /api/v1/diagnostics/bundle` untuk menyimpan snapshot JSON sebagai artifact.

## 8. Correlation IDs

- TUI membuat `client_request_id`.
- API membuat atau meneruskan `request_id`.
- Job memiliki `correlation_id` yang tetap sepanjang workflow.
- Tool, check, review, dan publication memiliki child span dan entity ID masing-masing.
