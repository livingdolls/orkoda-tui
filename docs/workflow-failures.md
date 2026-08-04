# Workflow failure diagnosis

A workflow moves to **Needs You** when a durable stage reaches `FAILED`. The Board keeps the failure evidence produced by the daemon and presents it before a retry.

## Board behavior

Selecting a failed card shows a stage-aware summary containing the persisted failure code and message. Pressing Enter opens **See why it failed**. The detail screen can combine evidence from:

- workflow failure metadata;
- isolated workspace preparation;
- Executor Agent runs;
- failed formatter, lint, typecheck, test, and build steps;
- Reviewer Agent runs.

After resolving the reported cause, return to the Board and choose **Retry workflow** from the action menu. Retry resumes the failed durable stage; it does not require recreating the plan.

## Common workspace failure

Workspace preparation requires the registered source repository to be clean. Inspect it with:

```bash
git -C <repository-path> status --short
```

Commit, stash, or intentionally remove the reported changes before retrying. The exact Board message remains the source of truth because failures can also originate from the Executor, checks, review, or publication stages.
