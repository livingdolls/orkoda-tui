# Version-bound human approval

Human decisions are durable domain records. They are not anonymous workflow status changes.

## Immutable binding

Every decision is bound to:

- workflow job ID and workflow version;
- completed execution ID and execution version;
- completed Reviewer run;
- latest patch checkpoint;
- base commit SHA;
- patch SHA-256 checksum;
- Reviewer verdict.

The API rejects stale or mismatched fingerprints. A decision for one execution version cannot be reused after a revision produces another execution.

## Decisions

Supported decisions:

- `APPROVE` advances `WAITING_FOR_APPROVAL` to `APPROVED`;
- `REQUEST_REVISION` records instructions, advances through `REVISION_REQUIRED`, and atomically requests the next durable `workflow.execute` dispatch through the existing transition table;
- `REJECT` advances the workflow to terminal `REJECTED`.

A unique `(workflow_job_id, execution_version)` constraint allows only one human decision for each execution snapshot.

## Reviewer override

The human remains authoritative. Approving a Reviewer `REQUEST_REVISION` verdict requires both:

- `review_override: true`;
- a non-empty approval note explaining the override.

The override flag is persisted and visible in the Jobs screen.

## Crash recovery

A decision is inserted with `PENDING` before workflow transitions are attempted. It becomes `APPLIED` only after the target transition completes.

For revision requests, a retry can resume from either:

- `WAITING_FOR_APPROVAL`;
- `REVISION_REQUIRED` after the first transition;
- a later state when the revision dispatch already advanced the workflow.

The same execution snapshot cannot receive a different decision after a crash or concurrent request.

## API

Read-only endpoints:

- `GET /api/v1/jobs/:jobID/decisions`
- `GET /api/v1/decisions/:decisionID`

Mutation endpoints:

- `POST /api/v1/jobs/:jobID/approve`
- `POST /api/v1/jobs/:jobID/request-revision`
- `POST /api/v1/jobs/:jobID/reject`

Mutation body:

```json
{
  "expected_version": 8,
  "execution_version": 1,
  "base_commit_sha": "abc123",
  "patch_checksum": "sha256:...",
  "note": "Decision reason or revision instructions",
  "review_override": false
}
```

## OpenTUI

On the Jobs screen:

- `↑↓` or `j/k` selects a workflow;
- `a` opens approve;
- `v` opens request revision;
- `x` opens reject;
- `o` acknowledges a Reviewer override inside the approve composer;
- `Ctrl+S` applies the displayed bound decision;
- `Esc` cancels the composer.

The composer displays the complete base SHA and patch checksum before submission.
