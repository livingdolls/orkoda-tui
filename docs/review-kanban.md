# Review Kanban

The Review screen is a read-only projection of the durable workflow, execution, check, review, and approval records. It does not introduce a second state machine.

Columns:

1. Awaiting Review
2. AI Reviewing
3. Issues Found
4. Revision
5. Re-review
6. Approval
7. Approved

Each card shows the immutable Executor and Reviewer provider/model snapshots, execution version, review cycle, blocking issue count, and check summary. Enter opens the existing evidence and approval detail. A failed review stage can be retried with `T`; the workflow retry target determines whether workspace preparation, execution, checks, review, or publication resumes.

For revision cycles, findings are compared by their stable issue key and shown as new, still present, partially resolved, or resolved. Reviewer access remains read-only. Human approval remains bound to the latest execution version, base commit, checkpoint, and patch checksum.
