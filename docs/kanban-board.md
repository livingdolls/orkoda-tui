# Unified Kanban Workspace

## Goal

The Board is the default Orkoda experience. A non-technical user should not need to understand the difference between a plan, planning run, workflow job, execution, check run, review run, or approval decision.

One work card follows the same requirement through the complete lifecycle:

```text
Planning -> Ready -> Working -> Needs You -> Done
```

The Board is a workflow projection, not a generic project-management system. Cards move from durable daemon state. Users cannot drag a card into an invalid state or bypass approval and publication guards.

## Navigation

The top-level product areas are:

1. Board
2. Agents
3. Settings
4. System

Normal usage stays inside Board. Agents, Settings, and System are advanced configuration and diagnostics areas.

Board controls:

| Key | Action |
|---|---|
| Left / Right | Move between columns |
| Up / Down | Select a card |
| Enter | Run the primary next action |
| Space | Open all valid actions |
| N | Create work for the current project |
| Shift+N | Add a Git project |
| Tab / Shift+Tab | Cycle the project filter |
| F | Toggle active-only and all work |
| R | Reload summaries |
| Escape | Close a dialog or return to the Board |

Approval detail retains the explicit controls:

- `A`: approve.
- `V`: request revision.
- `X`: reject.
- `E`: take over or release the isolated workspace.

## Column mapping

### Planning

Plan-only states:

- `DRAFT`
- `PLANNING`

### Ready

Plan states:

- `READY`
- `APPROVED`

Workflow states:

- `READY`
- `WORKSPACE_PREPARING`
- `QUEUED`

### Working

- `EXECUTING`
- `CHECKING`
- `REVIEWING`
- `APPROVED`
- `PUBLISHING`

### Needs You

- plan `NEEDS_INPUT`
- workflow `WAITING_FOR_APPROVAL`
- workflow `REVISION_REQUIRED`
- workflow `FAILED`

### Done

- plan `ARCHIVED`
- workflow `COMPLETED`
- workflow `REJECTED`
- workflow `CANCELLED`

Every status is mapped by a pure function with exhaustive tests. Adding a new status requires an explicit Board decision.

## Stable card identity

A card uses the plan ID as its stable identity:

```text
plan:<plan-id>
```

The identity does not change when a workflow is created. This lets selection follow a card when a live event moves it to another column.

When several workflow attempts exist for one plan, the Board displays the most recently updated workflow. Older attempts remain durable and accessible through daemon data.

## Data loading

The Board loads only:

- projects;
- plans for each project;
- current workflow aggregates from `GET /api/v1/projects/:projectID/board`.

The Board endpoint returns workflow jobs only. It intentionally excludes executions, check output, review issues, artifacts, and diff content.

When a workflow card opens, the detail view lazy-loads:

- latest execution and checkpoint;
- latest check run and steps;
- latest review and issues;
- current workspace;
- bounded unified diff;
- approval snapshot.

This replaces the old Jobs screen fan-out, which loaded detailed evidence for every job before the user selected one.

The TUI falls back to the existing jobs endpoint while a local daemon is being upgraded.

## Live updates

The application-level SSE connection is passed into the Board. A durable activity event schedules a summary reload.

Selection is stored by stable card ID. When the refreshed status moves that card:

1. the Board locates the same ID;
2. activates the new column;
3. selects the card at its new index;
4. keeps the user in context.

SSE reconnect and replay remain responsible for recovering missed durable events.

## Planning flow

Creating work opens the existing Plan Editor inside Board.

The primary action on a draft performs the prerequisites in order:

1. use or generate the repository summary;
2. use or create the normalized planning context;
3. start the Planning Agent;
4. open Planning Questions immediately when input is required;
5. return to Board after the run completes.

A ready plan starts a workflow from the repository's current concrete branch.

## Workflow detail and approval

The detail view presents information in user-oriented order:

1. current status;
2. pipeline progress;
3. review summary;
4. checks;
5. review findings;
6. changed files and diff;
7. technical fingerprints.

Approval still binds the decision to:

- workflow version;
- execution version;
- base commit SHA;
- patch checksum;
- reviewer verdict and explicit override acknowledgement.

The Board changes presentation only. It does not weaken the workflow state machine, immutable evidence, sandbox, or publication boundary.

## Responsive behavior

- At 150 columns or wider, show all five columns.
- From 100 to 149 columns, show three columns centered around the active column.
- Below 100 columns, show one active column.

Left and right always navigate the logical five-column board, even when some columns are outside the current viewport.

## Tests

Required coverage:

- plan and workflow status-to-column mapping;
- contextual action mapping;
- stable card identity;
- numeric top-level navigation;
- Board summary API response;
- real daemon E2E from a `WAITING_FOR_APPROVAL` card through persisted approval;
- compact terminal rendering after approval.
