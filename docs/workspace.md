# Isolated Git Workspaces

## Purpose

Each workflow job executes inside a dedicated Git worktree rather than the developer's registered working tree. The workspace is pinned to the workflow's immutable `base_commit_sha` and stored under a daemon-managed root.

Default location:

```text
.orkoda/workspaces/<workflow-job-id>
```

Configuration:

```bash
ORKODA_WORKSPACE_DIR=.orkoda/workspaces
ORKODA_WORKSPACE_LEASE_TTL=5m
```

The configured workspace root must not overlap a registered source repository. A nested workspace would make the source repository dirty and could expose daemon runtime files to Git operations.

## Persistence

The `workspaces` table stores one workspace per workflow job:

- workflow, project, and repository IDs
- managed filesystem path
- pinned base commit SHA
- current workspace HEAD SHA
- lifecycle status and dirty state
- lease owner, opaque lease token, and expiry
- bounded failure message
- creation and update timestamps

The workflow foreign key uses `ON DELETE CASCADE`. Deleting a database row does not remove the filesystem worktree; physical cleanup remains an explicit lifecycle operation so database deletion cannot silently destroy local files.

## Lifecycle

```text
REQUESTED
    ↓ acquire preparation lease
PREPARING
    ↓ detached worktree verified
READY
    ↓ future Executor write lease
WRITE_LOCKED
    ↓ execution complete
READY
    ↓ workflow terminal/cleanup
ARCHIVED

REQUESTED / PREPARING
    └── preparation failure → FAILED → retry lease → PREPARING
```

This change implements `REQUESTED`, `PREPARING`, `READY`, and `FAILED`. `WRITE_LOCKED` and `ARCHIVED` are reserved for Executor and cleanup handlers.

## Worktree creation

The adapter performs these checks before creating a worktree:

1. Resolve the registered source repository without accepting a symlink path.
2. Resolve the workspace destination and reject source/destination overlap.
3. Verify `base_commit_sha^{commit}` exists locally.
4. Reject a dirty source working tree, including untracked files.
5. Prune stale Git worktree registrations.
6. Run `git worktree add --detach <path> <commit>` without a shell.
7. Verify exact worktree root, HEAD SHA, and clean status.

A workspace path that already exists is never overwritten. It must be an exact Git worktree at the pinned base commit and must be clean. Otherwise preparation fails.

## SQLite lease

A preparation lease contains:

- stable worker owner ID
- random opaque token
- absolute expiry timestamp

The token is required for renewal, state mutation, and release. This prevents a stale worker from releasing or updating a lease acquired later by another daemon process.

Expired leases can be taken over. Active leases owned by another worker return a retryable lease-contention error.

## Durable handler

The queue handler consumes:

```text
workflow.prepare_workspace
```

Expected payload:

```json
{
  "workflow_job_id": "...",
  "workflow_version": 2,
  "action": "START",
  "target_status": "WORKSPACE_PREPARING"
}
```

The handler verifies workflow ID, version, and status before performing side effects.

### Idempotency

- A dispatch for an already advanced workflow completes as a no-op.
- An existing `READY` workspace is inspected and the workflow transition is resumed without creating another worktree.
- A workspace created before a daemon crash is reused only when root, HEAD, base commit, and clean state match.
- The transition to `WORKSPACE_READY` uses optimistic workflow versioning.
- A competing handler that already advanced the workflow causes the late handler to complete as a no-op.

Successful preparation transitions the workflow:

```text
WORKSPACE_PREPARING → QUEUED
```

The transition atomically enqueues the existing `workflow.execute` durable dispatch.

## Failure and recovery

Worktree failures mark the workspace `FAILED`, retain a bounded failure message, and return an error to the scheduler. The durable queue then applies its configured retry and dead-job behavior.

Context cancellation leaves the queue job recoverable by the scheduler's stale-running-job mechanism. Lease expiry allows another daemon process to resume preparation.

## Activity events

The handler records:

```text
workspace.preparing
workspace.ready
workspace.failed
workspace.lease_release_failed
```

Events contain workflow/workspace correlation, repository ID, version, queue attempt, HEAD SHA, and safe failure metadata. They do not contain repository file content or command environment values.

## Read API

```http
GET /api/v1/jobs/:jobID/workspace
GET /api/v1/projects/:projectID/workspaces
```

The API is read-only. Lease acquisition and lifecycle mutations are internal daemon operations.

## TUI

The Jobs screen displays:

- workspace lifecycle status
- current HEAD and dirty state
- managed path
- active lease owner and expiry
- preparation failure message

## Deferred work

The following remain separate tasks:

- Executor write lease and periodic renewal
- canonical file-path guard inside the workspace
- patch checkpoints and checksums
- terminal workspace archive and cleanup handlers
- orphan filesystem reconciliation
- submodule policy
