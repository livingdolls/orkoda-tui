# Agent Configuration and Tool Policy

## Scope

Agent settings are project-scoped and persisted in SQLite. Every project has exactly one configuration and one tool policy for each role:

- `PLANNER`
- `EXECUTOR`
- `REVIEWER`

The aggregate is versioned so OpenTUI and other clients cannot silently overwrite a newer edit.

## Persistence

```text
projects
   └── agent_settings (versioned aggregate)
       ├── agent_configs (one row per role)
       └── tool_policies (one row per role)
```

Defaults are created lazily the first time settings are read. Existing projects therefore receive safe defaults without a destructive backfill migration.

Deleting a project cascades to the aggregate, agent configurations, and tool policies.

## Agent configuration

Each role stores:

```json
{
  "role": "EXECUTOR",
  "provider": "",
  "model": "",
  "temperature": 0.1,
  "max_output_tokens": 8192,
  "enabled": true,
  "system_instruction": ""
}
```

An empty provider and model means the role inherits the daemon default. Provider and model must either both be empty or both be set.

## Tool policy

Each role stores:

```json
{
  "role": "EXECUTOR",
  "allowed_tools": [
    "file_read",
    "file_search",
    "file_patch",
    "file_create",
    "file_delete",
    "git_status",
    "git_diff"
  ],
  "allowed_command_profiles": [],
  "network_access": "DISABLED",
  "filesystem_access": "WORKSPACE_WRITE",
  "command_timeout_ms": 120000,
  "max_command_output_bytes": 1048576,
  "max_file_bytes": 2097152,
  "max_patch_bytes": 4194304
}
```

Supported tools:

```text
file_read
file_search
file_patch
file_create
file_delete
git_status
git_diff
command_run
```

`command_run` is valid only when at least one named command profile is allowed. Raw shell strings are not persisted as a policy substitute.

Planner and Reviewer remain read-only and cannot enable tool network access. Only Executor may receive write tools, workspace write access, or network access.

## Defaults

Defaults are deny-by-default:

- all roles inherit the daemon provider and model;
- network is disabled;
- Planner has no tools;
- Reviewer receives read-only inspection tools;
- Executor receives workspace file and Git inspection tools;
- `command_run` is disabled until explicit command profiles are configured.

## API

```text
GET /api/v1/projects/:projectID/agent-settings
PUT /api/v1/projects/:projectID/agent-settings
```

The update body replaces the complete aggregate:

```json
{
  "expected_version": 3,
  "agents": [],
  "tool_policies": []
}
```

A stale `expected_version` returns HTTP `409`. Validation failures return HTTP `400` and do not partially update any role.

Successful updates emit:

```text
agent.settings_updated
```

The activity event contains the project ID, new version, and affected roles. It does not contain system instructions, provider credentials, or command content.

## OpenTUI

The **Agents** screen supports:

```text
↑↓ / j k   select project
Tab         select role
e           toggle role enabled
n           cycle Executor network policy
f           toggle Executor filesystem policy
s           save the complete versioned aggregate
r           reload
```

Provider, model, allowed tools, command profiles, and numeric limits remain available through the local API until a full form editor is added.
