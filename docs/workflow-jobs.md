# Workflow Job Aggregate

## Purpose

`workflow_jobs` is the source of truth for the business lifecycle of a software-development workflow. The existing `jobs` table remains a generic durable dispatch queue.

The separation is intentional:

```text
workflow_jobs
  business status, version, limits, failure state, human decision
        │
        │ atomic transition
        ▼
jobs
  short-lived durable instruction for a registered stage handler
```

A queue retry must not silently change the business state, and a business transition must not be committed without its next durable dispatch.

## Creation Snapshot

A workflow job pins:

- project ID;
- accepted plan and exact plan-version ID;
- repository ID;
- base branch;
- base commit SHA;
- workflow limits.

Only plans in `READY` or `APPROVED` status can create a workflow job. The plan, repository, and project relationship is verified inside the creation transaction.

The default limits are:

```json
{
  "max_revisions": 3,
  "max_stage_attempts": 3,
  "max_tool_calls": 120,
  "wall_clock_seconds": 3600
}
```

## Status Model

```text
READY
  │ START
  ▼
WORKSPACE_PREPARING
  │ WORKSPACE_READY
  ▼
QUEUED
  │ EXECUTION_STARTED
  ▼
EXECUTING
  │ EXECUTION_COMPLETED
  ▼
CHECKING
  │ CHECKS_COMPLETED
  ▼
REVIEWING
  │ REVIEW_COMPLETED
  ▼
WAITING_FOR_APPROVAL
  ├─ APPROVE ─────────────► APPROVED
  ├─ REQUEST_REVISION ────► REVISION_REQUIRED
  └─ REJECT ──────────────► REJECTED

REVISION_REQUIRED
  │ QUEUE_REVISION
  ▼
QUEUED

APPROVED
  │ PUBLISH
  ▼
PUBLISHING
  │ PUBLICATION_COMPLETED
  ▼
COMPLETED
```

Operational statuses can use `FAIL`:

- `WORKSPACE_PREPARING`;
- `QUEUED`;
- `EXECUTING`;
- `CHECKING`;
- `REVIEWING`;
- `PUBLISHING`.

`FAILED` stores the previous operational status as `retry_status`. `RETRY` returns to that exact status and creates the corresponding dispatch.

`CANCEL` is accepted from any non-terminal status. `COMPLETED`, `REJECTED`, and `CANCELLED` are terminal.

## Optimistic Concurrency

Every mutation requires the current workflow version:

```json
{
  "expected_version": 4
}
```

A successful transition increments the version exactly once. A stale version returns HTTP `409` and cannot write a transition or enqueue a dispatch.

`workflow_job_transitions` has a unique key on:

```text
(workflow_job_id, workflow_version)
```

This makes the transition timeline immutable and prevents two transitions from claiming the same aggregate version.

## Atomic Dispatch

Actions that require asynchronous work enqueue a dispatch in the same SQLite transaction as the workflow transition:

| Action | Dispatch type |
|---|---|
| `START` | `workflow.prepare_workspace` |
| `WORKSPACE_READY` | `workflow.execute` |
| `QUEUE_REVISION` | `workflow.execute` |
| `EXECUTION_COMPLETED` | `workflow.run_checks` |
| `CHECKS_COMPLETED` | `workflow.review` |
| `PUBLISH` | `workflow.publish` |

The dispatch payload contains only correlation data:

```json
{
  "workflow_job_id": "...",
  "workflow_version": 2,
  "action": "START",
  "target_status": "WORKSPACE_PREPARING"
}
```

Repository content, prompts, credentials, patches, and large execution context are not copied into the queue payload.

## Registered Handler Safety

The scheduler asks the SQLite queue only for job types that have registered handlers. A workflow dispatch whose capability has not been implemented remains `QUEUED` and is not consumed as an unknown job.

The current daemon registers only `system.noop`. Therefore workspace, executor, checks, reviewer, and publication dispatches are persisted but intentionally remain queued until their handlers are added.

Future handlers must validate both:

- `workflow_job_id` exists;
- the current workflow version and status still match the dispatch payload.

A stale dispatch becomes a successful no-op rather than repeating side effects.

## API

```text
POST /api/v1/projects/:projectID/jobs
GET  /api/v1/projects/:projectID/jobs
GET  /api/v1/jobs/:jobID
GET  /api/v1/jobs/:jobID/transitions

POST /api/v1/jobs/:jobID/start
POST /api/v1/jobs/:jobID/cancel
POST /api/v1/jobs/:jobID/retry
POST /api/v1/jobs/:jobID/approve
POST /api/v1/jobs/:jobID/request-revision
POST /api/v1/jobs/:jobID/reject
POST /api/v1/jobs/:jobID/publish
```

There is deliberately no endpoint that accepts an arbitrary target status. Public operations map to predefined domain actions and the transition table remains enforced in the repository.

Internal stage handlers will later use the same repository with internal actions such as `WORKSPACE_READY`, `EXECUTION_STARTED`, `CHECKS_COMPLETED`, and `REVIEW_COMPLETED`.

## OpenTUI

The Jobs screen reads persisted workflow aggregates and displays:

- project name;
- business status;
- workflow version;
- execution version;
- revision count and limit;
- pinned branch and base commit;
- current durable dispatch ID;
- failure message when present.

This screen is currently read-only. Human decision controls will be introduced with approval and review persistence.
