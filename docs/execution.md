# Execution Foundation and Safe Workspace Tools

## Purpose

The `workflow.execute` stage turns a prepared isolated worktree into a durable execution snapshot. Business workflow state, execution state, tool runs, workspace lease ownership, and patch checkpoints remain separate persisted concerns.

```text
workflow.execute dispatch
        ↓
validate workflow version and status
        ↓
transition QUEUED → EXECUTING
        ↓
create or resume immutable execution snapshot
        ↓
acquire workspace WRITE_LOCKED lease
        ↓
run policy-enforced tools
        ↓
persist SHA-256 patch checkpoint
        ↓
release write lease
        ↓
transition EXECUTING → CHECKING
```

## Persistence

The migration adds:

- `executions`;
- `tool_runs`;
- `patch_checkpoints`.

An execution pins the workflow version, execution version, plan version, workspace, base commit, agent-settings version, provider, and model. The uniqueness constraint `(workflow_job_id, execution_version)` prevents duplicate executions for the same revision.

Tool runs are written before execution and completed or failed afterwards. Activity events contain summaries only; tool input and output bodies remain bounded JSON summaries in SQLite.

Patch checkpoints store:

- base commit SHA;
- current workspace HEAD;
- SHA-256 patch checksum;
- patch byte size;
- changed-file list;
- bounded patch text.

## Workspace write lease

Preparation and mutation leases use the same workspace lease columns but different status boundaries:

```text
READY
  ↓ AcquireWrite
WRITE_LOCKED
  ↓ ReleaseWrite
READY
```

The write lease has an owner, random token, and expiry. The handler renews the lease every third of its TTL. Renewal failure cancels the runner. Release requires the active token and persists HEAD and dirty state.

Expired `WRITE_LOCKED` leases can be taken over. A stale worker token cannot renew or release a newer lease.

## Path guard

Every file tool resolves a workspace-relative path through `PathGuard` before touching the filesystem. It rejects:

- absolute paths;
- `..` traversal;
- null bytes;
- paths outside the workspace root;
- `.git` internals;
- symlink components;
- sockets, devices, and named pipes.

`file_delete` only deletes regular files and never recursively deletes directories.

## Tools

The initial in-process tools are:

```text
file_read
file_search
file_patch
file_create
file_delete
git_status
git_diff
```

Each call is checked against the persisted Executor `ToolPolicy`:

- `allowed_tools`;
- `filesystem_access`;
- `max_file_bytes`;
- `max_patch_bytes`;
- workflow `max_tool_calls`.

Git commands run directly without a shell and use a constrained environment with terminal prompting disabled.

## Handler idempotency

The durable handler accepts `QUEUED` and crash-resume `EXECUTING` states.

- A workflow already past execution is a successful stale no-op.
- A duplicate dispatch reuses the unique execution snapshot.
- A completed execution resumes the workflow transition without running tools again.
- A final queue attempt failure moves the workflow to `FAILED`.
- A non-final failure remains retryable through the durable queue.

## Scripted runner

The daemon currently registers a deterministic read-only `ScriptedRunner`. It records `git_status` and `git_diff`, creates a checkpoint, and advances the workflow to checks. This validates durability and security boundaries before an autonomous LLM Executor loop is introduced.

## Read-only API

```text
GET /api/v1/jobs/:jobID/executions
GET /api/v1/executions/:executionID
GET /api/v1/executions/:executionID/tool-runs
GET /api/v1/executions/:executionID/checkpoints
```

No public endpoint can invoke an individual tool or acquire a workspace lease.
