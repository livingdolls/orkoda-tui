# Product Roadmap

## Phase 0 — Product and Security Design

### Deliverables

- Product scope khusus software development.
- OpenTUI interaction model.
- Workflow state machine.
- Agent and tool contracts.
- Repository trust and sandbox threat model.
- Initial architecture decisions.

### Exit Criteria

- Tidak ada use case non-software dalam MVP.
- Approval and publication rules disepakati.

## Phase 1 — OpenTUI Foundation

### Features

- TUI shell, navigation, command palette, config, and diagnostics.
- Golang local daemon/API.
- Project and repository list.
- Database and event protocol foundation.

### Exit Criteria

- TUI dapat terhubung, membuat project, dan membuka repository metadata.

## Phase 2 — Repository and Planning

### Features

- Local repository inspection.
- Git branch and base commit selection.
- Requirement editor.
- Planning Agent.
- Structured plan review and acceptance.

### Exit Criteria

- Developer dapat menghasilkan plan yang menunjuk affected areas dan test strategy.

## Phase 3 — Workspace and Executor

### Features

- Isolated worktree/clone.
- File read/search/patch.
- Executor tool loop.
- Command sandbox.
- Tool timeline and live progress.

### Exit Criteria

- Executor dapat menghasilkan patch tanpa mengubah source repository.

## Phase 4 — Checks and Review

### Features

- Formatter, linter, type check, test, and build configuration.
- Check runner.
- Diff viewer.
- Reviewer Agent.
- Severity, criteria, and blocking policy.

### Exit Criteria

- User dapat melihat evidence teknis dan review independen di TUI.

## Phase 5 — Human Approval and Revision

### Features

- Approve, override, request revision, reject, and take over.
- Immutable execution versions.
- Reviewer issue selection.
- Side-by-side diff comparison.

### Exit Criteria

- Revision loop berjalan sampai user menyetujui checksum tertentu.

## Phase 6 — Git Publication MVP

### Features

- Local commit.
- Branch naming.
- Git provider authentication.
- Push branch.
- Draft pull request.
- Publication conflict and idempotency handling.

### Exit Criteria

- Draft PR hanya dapat dibuat dari approved execution.

## Phase 7 — Production Readiness

### Features

- Worker recovery and DLQ.
- Workspace recovery.
- Secret redaction.
- Sandbox hardening.
- Metrics, tracing, alerting, and debug bundle.
- Signed releases.

### Exit Criteria

- Recovery and security tests lulus.
- Release candidate dapat digunakan pada repository non-kritis.

## Phase 8 — Advanced Code Intelligence

### Features

- Language server integration.
- Symbol graph and dependency-aware context.
- Test impact analysis.
- Repository memory and architecture map.
- Large monorepo context strategy.

## Phase 9 — Multi-Agent Software Teams

### Features

- Specialized backend, frontend, mobile, QA, security, and DevOps agents.
- Parallel task execution with file ownership.
- Review quorum and specialist review.
- Dependency graph orchestration.

## Phase 10 — Team and Enterprise

### Features

- Organization and RBAC.
- Shared policies and approved command profiles.
- SSO and audit export.
- Centralized remote workers.
- Usage quota and billing.
- Self-hosted control plane.
