# Database Design

## 1. Prinsip

- PostgreSQL menjadi source of truth.
- ID menggunakan UUID.
- Execution, review, approval, dan publication bersifat immutable.
- JSONB hanya untuk konfigurasi fleksibel; relasi penting tetap dinormalisasi.
- Workspace dapat direkonstruksi dari repository base commit dan patch artifact.

## 2. Core Tables

### users

```sql
CREATE TABLE users (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### projects

```sql
CREATE TABLE projects (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL REFERENCES users(id),
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    global_instruction TEXT NOT NULL DEFAULT '',
    default_base_branch TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### repositories

```sql
CREATE TABLE repositories (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    provider TEXT NOT NULL,
    remote_url TEXT,
    local_path TEXT,
    default_branch TEXT NOT NULL,
    trust_level TEXT NOT NULL DEFAULT 'RESTRICTED',
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (remote_url IS NOT NULL OR local_path IS NOT NULL)
);
```

### plans and plan_versions

```sql
CREATE TABLE plans (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'DRAFT',
    current_version INT NOT NULL DEFAULT 1,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plan_versions (
    id UUID PRIMARY KEY,
    plan_id UUID NOT NULL REFERENCES plans(id),
    version INT NOT NULL,
    requirement_markdown TEXT NOT NULL,
    structured_plan JSONB NOT NULL,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(plan_id, version)
);
```

### agents

```sql
CREATE TABLE agents (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    system_instruction TEXT NOT NULL,
    configuration JSONB NOT NULL DEFAULT '{}',
    tool_policy JSONB NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### jobs

```sql
CREATE TABLE jobs (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    repository_id UUID NOT NULL REFERENCES repositories(id),
    plan_id UUID NOT NULL REFERENCES plans(id),
    plan_version INT NOT NULL,
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    current_stage TEXT NOT NULL,
    base_branch TEXT NOT NULL,
    base_commit_sha TEXT NOT NULL,
    executor_agent_id UUID NOT NULL REFERENCES agents(id),
    reviewer_agent_id UUID NOT NULL REFERENCES agents(id),
    current_execution_version INT NOT NULL DEFAULT 0,
    revision_count INT NOT NULL DEFAULT 0,
    limits JSONB NOT NULL DEFAULT '{}',
    version BIGINT NOT NULL DEFAULT 1,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### workspaces

```sql
CREATE TABLE workspaces (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id),
    kind TEXT NOT NULL,
    location_ref TEXT NOT NULL,
    base_commit_sha TEXT NOT NULL,
    branch_name TEXT NOT NULL,
    status TEXT NOT NULL,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    current_patch_checksum TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ
);
```

### executions

```sql
CREATE TABLE executions (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id),
    workspace_id UUID NOT NULL REFERENCES workspaces(id),
    agent_id UUID NOT NULL REFERENCES agents(id),
    version INT NOT NULL,
    status TEXT NOT NULL,
    input_snapshot JSONB NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    changed_files JSONB NOT NULL DEFAULT '[]',
    patch_artifact_id UUID,
    patch_checksum TEXT,
    usage JSONB NOT NULL DEFAULT '{}',
    error_code TEXT,
    error_message TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    UNIQUE(job_id, version)
);
```

### tool_runs

```sql
CREATE TABLE tool_runs (
    id UUID PRIMARY KEY,
    execution_id UUID NOT NULL REFERENCES executions(id),
    sequence INT NOT NULL,
    tool_name TEXT NOT NULL,
    input_redacted JSONB NOT NULL,
    status TEXT NOT NULL,
    exit_code INT,
    output_summary TEXT,
    log_artifact_id UUID,
    policy_decision JSONB NOT NULL DEFAULT '{}',
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    UNIQUE(execution_id, sequence)
);
```

### check_definitions and check_runs

```sql
CREATE TABLE check_definitions (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    command TEXT NOT NULL,
    required BOOLEAN NOT NULL DEFAULT true,
    timeout_seconds INT NOT NULL DEFAULT 600,
    configuration JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE check_runs (
    id UUID PRIMARY KEY,
    execution_id UUID NOT NULL REFERENCES executions(id),
    check_definition_id UUID NOT NULL REFERENCES check_definitions(id),
    status TEXT NOT NULL,
    exit_code INT,
    summary TEXT,
    log_artifact_id UUID,
    duration_ms BIGINT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);
```

### reviews and review_issues

```sql
CREATE TABLE reviews (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id),
    execution_id UUID NOT NULL REFERENCES executions(id),
    reviewer_agent_id UUID NOT NULL REFERENCES agents(id),
    decision TEXT NOT NULL,
    score INT NOT NULL,
    summary TEXT NOT NULL,
    residual_risks JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE review_issues (
    id UUID PRIMARY KEY,
    review_id UUID NOT NULL REFERENCES reviews(id),
    severity TEXT NOT NULL,
    category TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    file_path TEXT,
    line_start INT,
    line_end INT,
    evidence TEXT,
    recommendation TEXT NOT NULL,
    is_blocking BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL DEFAULT 'OPEN'
);
```

### revision_requests

```sql
CREATE TABLE revision_requests (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id),
    execution_id UUID NOT NULL REFERENCES executions(id),
    review_id UUID REFERENCES reviews(id),
    requested_by UUID NOT NULL REFERENCES users(id),
    instruction TEXT NOT NULL,
    selected_issue_ids JSONB NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### approvals

```sql
CREATE TABLE approvals (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id),
    execution_id UUID NOT NULL REFERENCES executions(id),
    review_id UUID NOT NULL REFERENCES reviews(id),
    user_id UUID NOT NULL REFERENCES users(id),
    decision TEXT NOT NULL,
    notes TEXT,
    override_reason TEXT,
    base_commit_sha TEXT NOT NULL,
    patch_checksum TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### git_publications

```sql
CREATE TABLE git_publications (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id),
    approval_id UUID NOT NULL REFERENCES approvals(id),
    kind TEXT NOT NULL,
    provider TEXT NOT NULL,
    branch_name TEXT NOT NULL,
    commit_sha TEXT,
    pull_request_ref TEXT,
    status TEXT NOT NULL,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    UNIQUE(approval_id, kind, branch_name)
);
```

### artifacts, activity_events, outbox_events, processed_messages

Artifacts menyimpan patch, diff, command log, test report, coverage, dan archive. Activity events menyimpan timeline product. Outbox dan processed messages menjamin durable delivery dan idempotent consumption.

## 3. Indexes

```sql
CREATE INDEX idx_jobs_project_status ON jobs(project_id, status, updated_at DESC);
CREATE INDEX idx_workspaces_job_status ON workspaces(job_id, status);
CREATE INDEX idx_executions_job_version ON executions(job_id, version DESC);
CREATE INDEX idx_tool_runs_execution_sequence ON tool_runs(execution_id, sequence);
CREATE INDEX idx_check_runs_execution ON check_runs(execution_id, status);
CREATE INDEX idx_review_issues_review_severity ON review_issues(review_id, severity);
CREATE INDEX idx_activity_job_sequence ON activity_events(job_id, sequence);
CREATE INDEX idx_outbox_unpublished ON outbox_events(created_at) WHERE published_at IS NULL;
```

## 4. Integrity Rules

- Executor dan Reviewer agent ID tidak boleh sama.
- Approval harus menunjuk execution dan review terbaru.
- Approval patch checksum harus sama dengan workspace current patch checksum.
- Publication hanya dapat menggunakan approval berstatus approved.
- Satu workspace hanya memiliki satu active write lease.
- Path file dan artifact selalu project-scoped.
- Versi execution tidak dapat diubah setelah selesai.

## 5. Retention

- Approval, publication, dan audit event dipertahankan permanen sesuai kebijakan.
- Command log sensitif dapat memiliki retention lebih pendek.
- Workspace non-final dapat dibersihkan setelah patch dan metadata tersimpan.
- Final patch, check summary, review, dan approval harus tetap tersedia untuk audit.
