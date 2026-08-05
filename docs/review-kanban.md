# Unified Workflow Board

Review is part of the main Board. Orkoda does not expose a separate Review screen or a second state machine.

A card follows one durable workflow through these presentation columns:

1. Planning
2. Ready
3. Executing
4. Checking
5. Awaiting Review
6. AI Reviewing
7. Issues Found
8. Revision
9. Re-review
10. Approval
11. Done

The columns are read-only projections of plan, workflow, execution, check, review, approval, and publication records. Cards cannot be dragged manually.

Failures remain in the stage that failed. For example, a failed automated check remains in Checking, while a failed Reviewer run appears in Issues Found. Enter opens the shared workflow detail and Space opens every valid action. Retry uses the durable workflow retry target.

Review cards retain immutable Executor and Reviewer provider/model snapshots, execution version, review cycle, check totals, blocking findings, and previous-review comparison. Reviewer access remains read-only, and human approval remains bound to the latest execution version, base commit, checkpoint, and patch checksum.
