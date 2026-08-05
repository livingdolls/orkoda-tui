# Restarting a failed workflow

Every failed workflow exposes **Restart from beginning** on the Board.

Restart keeps the existing workflow, plan version, provider/model snapshots, transitions, executions, checks, reviews, iteration history, and checkpoint evidence. It does not create another workflow record.

The restart transition performs these steps:

1. move the failed workflow back to `WORKSPACE_PREPARING`;
2. acquire a dedicated restart lease without stealing an active writer;
3. force-remove the old isolated worktree;
4. recreate the worktree at the workflow's pinned `base_commit_sha`;
5. reset the revision counter and clear failure state;
6. queue the Executor and create a new execution version when execution begins.

The current workspace changes are discarded. Previously persisted checkpoints and artifacts remain available for audit.

**Retry workflow** continues to retry only the failed stage with the current workspace. **Continue Executor** continues a paused Executor with extra turns. **Restart from beginning** is the explicit clean-slate option for the same workflow.
