# API and TUI Protocol

## 1. Transport

OpenTUI mendukung dua transport:

- Local mode: Unix domain socket atau localhost HTTP.
- Remote mode: HTTPS dengan bearer token.

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

```text
POST /auth/device/start
POST /auth/device/complete
POST /auth/refresh
POST /auth/logout
GET  /auth/me
```

Local mode dapat menggunakan OS user identity dan local profile tanpa cookie browser. Token remote disimpan melalui OS keychain adapter bila tersedia.

## 3. Projects and Repositories

```text
POST   /projects
GET    /projects
GET    /projects/:projectId
PATCH  /projects/:projectId
POST   /projects/:projectId/archive
POST   /projects/:projectId/repositories
GET    /repositories/:repositoryId
POST   /repositories/:repositoryId/inspect
GET    /repositories/:repositoryId/branches
POST   /repositories/:repositoryId/trust
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
GET    /plans/:planId
POST   /plans/:planId/normalize
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
POST  /projects/:projectId/agents
GET   /projects/:projectId/agents
PATCH /agents/:agentId
POST  /agents/:agentId/test
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
GET  /jobs/:jobId/timeline
GET  /jobs/:jobId/versions
GET  /jobs/:jobId/workspace
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
GET /executions/:executionId/changed-files
GET /executions/:executionId/diff
GET /executions/:executionId/tool-runs
GET /executions/:executionId/check-runs
POST /executions/:executionId/checks/retry
```

Diff query mendukung file dan hunk pagination:

```text
GET /executions/:id/diff?path=internal/user/service.go&cursor=...
```

## 8. Reviews

```text
GET  /jobs/:jobId/reviews
GET  /reviews/:reviewId
POST /reviews/:reviewId/retry
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
POST /jobs/:jobId/approve-with-override
POST /jobs/:jobId/request-revision
POST /jobs/:jobId/reject
POST /jobs/:jobId/take-over
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
GET /jobs/:jobId/events
```

Event envelope:

```json
{
  "sequence": 1842,
  "type": "tool.completed",
  "job_id": "uuid",
  "occurred_at": "2026-07-31T06:00:00Z",
  "data": {}
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

Client reconnect menggunakan `after_sequence`.

## 12. Idempotency and Concurrency

Command mutation menerima:

```text
Idempotency-Key: unique-client-key
If-Match-Version: 17
```

Wajib untuk start, cancel, approval, revision, commit, push, dan pull request.

## 13. Status Codes

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
