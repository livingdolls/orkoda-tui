# Durable Reviewer Agent

`workflow.review` evaluates one immutable execution snapshot after repository checks finish.

## Snapshot

Every review run is bound to:

- workflow job and execution version;
- completed execution ID;
- completed check run ID;
- latest patch checkpoint and checksum;
- agent-settings version;
- provider and model.

The unique `(workflow_job_id, execution_version)` constraint prevents duplicate review runs. A retry must use the same snapshot or fail with a snapshot conflict.

## Bounded evidence

The Reviewer receives only:

- the original requirement, acceptance criteria, and constraints;
- the persisted implementation plan;
- the execution base commit and patch checksum;
- normalized changed-file paths;
- a bounded patch;
- bounded formatter, linter, typecheck, test, and build evidence.

Patch and check output truncation are explicitly represented in the prompt. Raw provider responses and prompts are not persisted.

## Structured result

The result contains:

- verdict: `APPROVE` or `REQUEST_REVISION`;
- summary;
- up to 100 issues;
- issue key, severity, category, blocking flag, title, and description;
- optional changed-file and line reference;
- acceptance-criteria references.

Validation rules include:

- issue keys are unique;
- file references must point to the checkpoint's changed files;
- paths must be repository-relative and traversal-safe;
- criteria references must use IDs supplied in the review context;
- critical issues must be blocking;
- `APPROVE` cannot contain a blocking issue;
- `REQUEST_REVISION` requires at least one blocking issue.

## Durability

A duplicate dispatch reuses the persisted review run. A completed review advances the workflow without another provider request. Provider or persistence failures use durable queue retry. The final infrastructure attempt moves the workflow to `FAILED`.

A successful review always advances the workflow from `REVIEWING` to `WAITING_FOR_APPROVAL`. The reviewer verdict is advisory evidence for the human approval stage; automatic approval or revision is intentionally not performed here. Human approval remains a separate, version-bound workflow operation.

## API

Read-only endpoints:

- `GET /api/v1/jobs/:jobID/reviews`
- `GET /api/v1/reviews/:reviewID`
- `GET /api/v1/reviews/:reviewID/issues`
