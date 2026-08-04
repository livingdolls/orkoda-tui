# API and TUI Protocol

## 1. Transport

Daemon lokal menyediakan dua transport:

- Local mode: Unix domain socket atau localhost HTTP.
- HTTP localhost dilindungi bearer token.

Remote SaaS transport dan device authentication belum termasuk MVP personal-local.

Base path remote:

```text
/api/v1
```

Response sukses:

```json
{"data": {}, "meta": {}}
```

Response error:

```json
{
  "error": {
    "code": "INVALID_STATE_TRANSITION",
    "message": "Job cannot be approved from its current status",
    "details": {}
  },
  "request_id": "req_..."
}
```

## 2. Authentication and Profiles

Setiap request `/api/v1` membawa `Authorization: Bearer <token>`. Daemon membuat token
0600 di `.orkoda/api.token`, atau menggunakan `ORKODA_API_TOKEN` dan
`ORKODA_API_TOKEN_FILE`. Route device/refresh/logout/me pada daftar lama adalah fitur
remote yang sengaja berada di luar MVP.

## 3. Projects and Repositories

```text
POST   /projects
GET    /projects
GET    /projects/:projectId
PATCH  /projects/:projectId
GET    /repositories/:repositoryId
GET    /repositories/:repositoryId/branches
GET    /repositories/:repositoryId/submodules
POST   /repositories/:repositoryId/trust
POST   /repositories/:repositoryId/summaries
GET    /repositories/:repositoryId/summaries/current
POST   /projects/:projectId/refresh
```

```json
{
  "name": "Operaya",
  "description": "Company management backend",
  "repository": {
    "provider": "LOCAL",
    "local_path": "/workspace/operaya",
    "default_branch": "main"
  }
}
```

## 4. Plans

```text
POST   /projects/:projectId/plans
GET    /projects/:projectId/plans
GET    /plans/:planId
PATCH  /plans/:planId
POST   /plans/:planId/normalize
GET    /plans/:planId/context
POST   /plans/:planId/versions
POST   /plans/:planId/accept
```

Structured plan:

```json
{
  "goal": "Add employee CSV import",
  "requirements": [],
  "constraints": [],
  "affected_areas": [],
  "implementation_steps": [],
  "test_strategy": [],
  "acceptance_criteria": [],
  "risks": [],
  "open_questions": []
}
```

## 5. Agents and Tool Policies

```text
GET /projects/:projectId/agent-settings
PUT /projects/:projectId/agent-settings
GET /llm/providers
GET /llm/policy

Planning Agent:

```text
POST /plans/:planId/planning-runs
GET  /plans/:planId/planning-runs/current
GET  /planning-runs/:runId
POST /planning-runs/:runId/answers
```
```

```json
{
  "name": "Go Backend Executor",
  "role": "EXECUTOR",
  "provider": "configured-provider",
  "model": "configured-model",
  "configuration": {
    "max_iterations": 20,
    "timeout_seconds": 1800,
    "token_limit": 150000
  },
  "tool_policy": {
    "allowed": ["file_read", "file_search", "file_patch", "git_diff", "command_run"],
    "network": "DENY",
    "command_profile": "GO_DEVELOPMENT"
  }
}
```

## 6. Jobs and Workspaces

```text
POST /projects/:projectId/jobs
GET  /projects/:projectId/jobs
GET  /jobs/:jobId
POST /jobs/:jobId/start
POST /jobs/:jobId/cancel
POST /jobs/:jobId/retry
POST /jobs/:jobId/publish
GET  /jobs/:jobId/transitions
GET  /jobs/:jobId/events
GET  /jobs/:jobId/workspace
GET  /projects/:projectId/workspaces
POST /jobs/:jobId/workspace/take-over
POST /jobs/:jobId/take-over
POST /workspaces/:workspaceId/release
POST /workspaces/:workspaceId/archive
POST /workspaces/cleanup
```

```json
{
  "plan_id": "uuid",
  "plan_version": 2,
  "repository_id": "uuid",
  "base_branch": "main",
  "executor_agent_id": "uuid",
  "reviewer_agent_id": "uuid",
  "limits": {
    "max_revisions": 4,
    "max_tool_calls": 120,
    "cost_limit_usd": 15,
    "wall_clock_seconds": 3600
  }
}
```

## 7. Executions, Tools, and Checks

```text
GET /jobs/:jobId/executions
GET /executions/:executionId
GET /executions/:executionId/iterations
GET /executions/:executionId/changed-files
GET /executions/:executionId/diff
GET /executions/:executionId/tool-runs
GET /executions/:executionId/checkpoints

GET /jobs/:jobId/checks
GET /checks/:checkId
GET /checks/:checkId/steps
GET /artifacts/:artifactKey
```

Diff query mendukung file dan hunk pagination:

```text
GET /executions/:id/diff?path=internal/user/service.go&cursor=...
```

## 8. Reviews

```text
GET  /jobs/:jobId/reviews
GET  /reviews/:reviewId
GET  /reviews/:reviewId/issues
```

```json
{
  "decision": "REVISION_REQUIRED",
  "score": 76,
  "summary": "Implementation works but authorization is incomplete.",
  "issues": [
    {
      "severity": "HIGH",
      "category": "SECURITY",
      "file_path": "internal/http/employee_handler.go",
      "line_start": 88,
      "title": "Missing tenant authorization",
      "evidence": "Update path trusts company_id without membership validation.",
      "recommendation": "Validate membership before repository call.",
      "is_blocking": true
    }
  ]
}
```

## 9. Human Decision

```text
POST /jobs/:jobId/approve
POST /jobs/:jobId/request-revision
POST /jobs/:jobId/reject
GET  /jobs/:jobId/decisions
```

```json
{
  "execution_id": "uuid",
  "review_id": "uuid",
  "patch_checksum": "sha256:...",
  "notes": "Approved after manual verification"
}
```

## 10. Git Publication

```text
POST /jobs/:jobId/publications/commit
POST /jobs/:jobId/publications/push
POST /jobs/:jobId/publications/pull-request
GET  /jobs/:jobId/publications
```

```json
{
  "approval_id": "uuid",
  "branch_name": "agent/employee-import",
  "commit_message": "feat(employee): add CSV import",
  "pull_request": {
    "draft": true,
    "target_branch": "main"
  }
}
```

## 11. Event Stream

```text
GET /events
GET /jobs/:jobId/events
```

Tanpa `Accept: text/event-stream`, endpoint mengembalikan replay JSON dengan
`after_sequence` dan `limit`. Dengan header tersebut (atau `stream=true`) endpoint
menjadi SSE dan mengirim `id`, `event`, `data`, serta keep-alive comment.

Event envelope:

```json
{
  "sequence": 1842,
  "type": "tool.completed",
  "job_id": "uuid",
  "created_at": "2026-07-31T06:00:00Z",
  "payload": {}
}
```

Event types:

- `workspace.preparing`.
- `workspace.ready`.
- `execution.started`.
- `tool.started`.
- `tool.completed`.
- `check.completed`.
- `review.completed`.
- `approval.required`.
- `revision.started`.
- `publication.completed`.

Client reconnect menggunakan `after_sequence`; subscriber yang tertinggal mengambil
ulang event durable dari SQLite sebelum menerima live notification.

## 12. Diagnostics and Artifacts

```text
GET  /status
GET  /metrics
GET  /diagnostics
POST /diagnostics/bundle
GET  /artifacts/:artifactKey
```

Bundle diagnostics disimpan sebagai artifact JSON yang sudah tidak memuat credential.
Artifact key hanya boleh berupa path relatif di bawah `.orkoda/artifacts`; traversal,
symlink, special file, dan response lebih besar dari 16 MiB ditolak.

## 13. Idempotency and Concurrency

Command mutation menerima:

```text
Idempotency-Key: unique-client-key
```

TUI membuat key tersebut untuk setiap mutation; daemon menyimpan hash request dan
response di SQLite selama 24 jam. `expected_version` dikirim di body action/approval
untuk optimistic concurrency. Mutation dengan key dan payload berbeda menghasilkan
`409`, sedangkan retry payload yang sama mengulang response durable.

## 14. Status Codes

- `200`: query atau synchronous command berhasil.
- `201`: resource dibuat.
- `202`: asynchronous command diterima.
- `400`: payload invalid.
- `401`: authentication diperlukan.
- `403`: permission atau policy menolak.
- `404`: resource tidak ditemukan.
- `409`: state, Git, lease, atau idempotency conflict.
- `422`: business rule gagal.
- `429`: rate atau budget limit.
- `500`: internal error.
- `503`: dependency sementara tidak tersedia.
