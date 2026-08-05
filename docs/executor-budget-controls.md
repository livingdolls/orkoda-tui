# Executor budget controls

Orkoda uses bounded Executor turns instead of an unbounded agent loop.

Default workflow budget:

- 32 total Executor turns
- 24 recorded tool calls
- one finalization-only turn reserved from the total
- pause after 3 consecutive tool errors
- pause after 4 successful write actions that do not change the workspace
- pause after the same action is repeated three times

When the Executor reaches a deterministic limit, the execution and workflow retain a structured `EXECUTOR_*` failure code. The Board projects that durable `FAILED` state as **Executor paused** and offers **Continue +8 turns** or **Continue +16 turns**. Continue reuses the same isolated workspace, preserves previous execution evidence, starts a new execution version, and clears the pause reason.

The final turn cannot call tools. It must return either `finish` or `needs_more_work`. Orkoda validates the final Git snapshot automatically, so the model no longer has to spend turns calling `git_status` and `git_diff` before finishing.

Executor iteration history is available through `GET /api/v1/executions/:executionID/iterations` and is displayed in the workflow detail screen.
