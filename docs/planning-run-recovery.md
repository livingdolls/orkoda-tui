# Planning run recovery

Planning Agent execution is synchronous with the daemon request and may take longer than a short client request timeout when a remote LLM provider retries or falls back.

The TUI allows planning requests to remain open for up to five minutes. While a plan is persisted as `PLANNING`, the Board refreshes periodically so a completed plan moves to **Ready** without manual card movement.

If a request is cancelled, the daemon persists a terminal planning-run status with a detached, bounded cleanup context and resets the plan to `DRAFT`, allowing the user to retry.

A `RUNNING` planning row cannot survive a daemon restart because no in-process LLM call remains attached to it. During startup, the daemon therefore marks such interrupted rows as `CANCELLED` and resets affected `PLANNING` plans to `DRAFT`. The user can then select the card and press **Enter** to start planning again.
