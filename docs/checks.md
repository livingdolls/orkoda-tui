# Durable checks

`workflow.run_checks` validates the completed execution inside its isolated workspace before review.

## Safety model

- Commands come only from built-in profiles; the LLM, API, and user input cannot provide arbitrary commands.
- Processes run directly without a shell.
- Network-dependent package resolution is disabled through the command environment.
- Each command has a timeout and a bounded combined stdout/stderr buffer.
- The workspace is protected by the existing write lease during checks.
- Check results are persisted per workflow execution version.

## Built-in profiles

For Go repositories:

- `go.format`: `gofmt -l` over repository Go files.
- `go.vet`: `go vet ./...`.
- `go.test`: `go test ./...`.

For Bun projects, only declared scripts with these names are considered:

- `lint:ts` or `lint`.
- `typecheck`.
- `test:ts` or `test`.
- `build`.

The script name is allowlisted; raw script content is never copied into the durable dispatch payload.

## Failure semantics

A formatter, linter, typecheck, test, or build failure is a valid check result. The workflow advances to review so the Reviewer Agent can explain the failure and request a revision.

Persistence, lease, cancellation, and dispatch failures are infrastructure failures. They use durable queue retries and move the workflow to `FAILED` when the final attempt is exhausted.

## Recovery

A check run is unique for `(workflow_job_id, execution_version)`. Passed and failed steps are terminal. Steps interrupted while `RUNNING`, or cancelled during shutdown, are reset to `PENDING` after the next worker acquires the workspace lease.
