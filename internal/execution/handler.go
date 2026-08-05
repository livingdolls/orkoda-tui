package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
	"github.com/livingdolls/orkoda-tui/internal/artifact"
	"github.com/livingdolls/orkoda-tui/internal/gitstate"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
	"github.com/livingdolls/orkoda-tui/internal/workspace"
)

type WorkflowStore interface {
	Get(context.Context, string) (workflowjob.Job, error)
	Transition(context.Context, string, workflowjob.TransitionInput) (workflowjob.Job, error)
}

type WorkspaceStore interface {
	GetByWorkflow(context.Context, string) (workspace.Workspace, error)
	AcquireWrite(context.Context, string, string, time.Duration) (workspace.Lease, error)
	Renew(context.Context, string, string, time.Duration) (workspace.Lease, error)
	Release(context.Context, string, string) error
	ReleaseWrite(context.Context, string, string, string, bool) (workspace.Workspace, error)
}

type AgentSettingsStore interface {
	Get(context.Context, string) (agentconfig.Settings, error)
}

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type Runner interface {
	Run(context.Context, RunContext) error
}

type RunContext struct {
	Execution Execution
	Workspace workspace.Workspace
	Tools     *RecordedTools
	Budget    ExecutorBudget
}

type Handler struct {
	workflows       WorkflowStore
	workspaces      WorkspaceStore
	settings        AgentSettingsStore
	executions      *Repository
	runner          Runner
	recorder        EventRecorder
	workerID        string
	leaseTTL        time.Duration
	defaultProvider string
	defaultModel    string
	artifactStore   artifact.Store
}

func NewHandler(
	workflows WorkflowStore,
	workspaces WorkspaceStore,
	settings AgentSettingsStore,
	executions *Repository,
	runner Runner,
	recorder EventRecorder,
	workerID string,
	leaseTTL time.Duration,
	defaultProvider string,
	defaultModel string,
	artifactStores ...artifact.Store,
) (*Handler, error) {
	if workflows == nil || workspaces == nil || settings == nil || executions == nil || runner == nil {
		return nil, fmt.Errorf("workflow, workspace, settings, execution, and runner dependencies are required")
	}
	if strings.TrimSpace(workerID) == "" || leaseTTL <= 0 {
		return nil, fmt.Errorf("worker ID and positive write lease TTL are required")
	}
	var artifactStore artifact.Store
	if len(artifactStores) > 0 {
		artifactStore = artifactStores[0]
	}
	return &Handler{
		workflows: workflows, workspaces: workspaces, settings: settings,
		executions: executions, runner: runner, recorder: recorder,
		workerID: workerID, leaseTTL: leaseTTL,
		defaultProvider: defaultProvider, defaultModel: defaultModel,
		artifactStore: artifactStore,
	}, nil
}

type dispatchPayload struct {
	WorkflowJobID   string             `json:"workflow_job_id"`
	WorkflowVersion int                `json:"workflow_version"`
	Action          workflowjob.Action `json:"action"`
	TargetStatus    workflowjob.Status `json:"target_status"`
}

func (h *Handler) HandleDurable(ctx context.Context, queueJob jobqueue.Job) error {
	payload, err := decodeExecutionDispatch(queueJob.PayloadJSON)
	if err != nil {
		return err
	}

	job, err := h.workflows.Get(ctx, payload.WorkflowJobID)
	if err != nil {
		return err
	}
	if job.Version < payload.WorkflowVersion {
		return fmt.Errorf("workflow version %d has not reached dispatch version %d", job.Version, payload.WorkflowVersion)
	}
	if job.Status != workflowjob.StatusQueued && job.Status != workflowjob.StatusExecuting {
		return nil
	}
	if job.CancellationRequested {
		return context.Canceled
	}

	job, err = h.ensureExecuting(ctx, job, payload, queueJob.ID)
	if err != nil {
		return err
	}
	if job.ExecutionVersion < 1 {
		return fmt.Errorf("workflow execution version is not initialized")
	}

	item, err := h.workspaces.GetByWorkflow(ctx, job.ID)
	if err != nil {
		return err
	}
	settings, err := h.settings.Get(ctx, job.ProjectID)
	if err != nil {
		return err
	}
	agent, policy, err := executorSnapshot(settings)
	if err != nil {
		return err
	}
	provider := strings.TrimSpace(job.Executor.Provider)
	model := strings.TrimSpace(job.Executor.Model)
	if provider == "" {
		provider = agent.Provider
	}
	if provider == "" {
		provider = h.defaultProvider
	}
	if model == "" {
		model = agent.Model
	}
	if model == "" {
		model = h.defaultModel
	}
	settingsVersion := job.AgentSettingsVersion
	if settingsVersion < 1 {
		settingsVersion = settings.Version
	}

	executionItem, _, err := h.executions.CreateOrGet(ctx, CreateInput{
		WorkflowJobID: job.ID, WorkflowVersion: job.Version,
		ExecutionVersion: job.ExecutionVersion, PlanVersionID: job.PlanVersionID,
		WorkspaceID: item.ID, BaseCommitSHA: job.BaseCommitSHA,
		AgentSettingsVersion: settingsVersion, Provider: provider, Model: model,
	})
	if err != nil {
		return err
	}
	if executionItem.Status == StatusCompleted {
		return h.finishWorkflow(ctx, job, executionItem, queueJob.ID)
	}
	if executionItem.Status == StatusFailed {
		return fmt.Errorf("execution %s is failed and requires workflow retry", executionItem.ID)
	}

	lease, err := h.workspaces.AcquireWrite(ctx, item.ID, h.workerID, h.leaseTTL)
	if err != nil {
		return err
	}
	released := false
	defer func() {
		if !released {
			persistCtx := context.WithoutCancel(ctx)
			if snapshot, snapshotErr := gitstate.Capture(persistCtx, item.Path, policy.MaxPatchBytes); snapshotErr == nil {
				_, _ = h.workspaces.ReleaseWrite(persistCtx, item.ID, lease.Token, snapshot.Head, snapshot.Dirty)
			} else {
				_ = h.workspaces.Release(persistCtx, item.ID, lease.Token)
			}
		}
	}()

	runCtx, cancel := workflowjob.WithWallClock(ctx, job)
	defer cancel()
	leaseErr := make(chan error, 1)
	go h.renewLease(runCtx, item.ID, lease.Token, cancel, leaseErr)
	cancellationErr := make(chan error, 1)
	cancellationDone := workflowjob.StartCancellationWatcher(
		runCtx, job.ID, 500*time.Millisecond, h.workflows.Get, cancel, cancellationErr,
	)
	defer func() {
		cancel()
		<-cancellationDone
	}()

	executionItem, err = h.executions.Start(ctx, executionItem.ID)
	if err != nil {
		return err
	}
	tools := &RecordedTools{
		repository: h.executions,
		execution:  executionItem,
		toolset:    Toolset{Root: item.Path, Policy: policy},
		maxCalls:   job.Limits.MaxToolCalls,
	}
	h.record(ctx, job.ID, "execution.started", map[string]any{
		"execution_id":           executionItem.ID,
		"execution_version":      executionItem.ExecutionVersion,
		"agent_settings_version": executionItem.AgentSettingsVersion,
	}, time.Now().UTC())

	runErr := h.runner.Run(runCtx, RunContext{
		Execution: executionItem, Workspace: item, Tools: tools,
		Budget: ExecutorBudget{
			MaxTurns:                 job.Limits.MaxExecutorTurns,
			MaxConsecutiveToolErrors: job.Limits.MaxConsecutiveToolErrors,
			MaxNoProgressTurns:       job.Limits.MaxNoProgressTurns,
		},
	})
	durableCancelled := false
	select {
	case renewalErr := <-leaseErr:
		if renewalErr != nil {
			runErr = renewalErr
		}
	default:
	}
	select {
	case cancellation := <-cancellationErr:
		if cancellation != nil {
			durableCancelled = errors.Is(cancellation, context.Canceled)
			runErr = cancellation
		}
	default:
	}
	if runErr != nil {
		persistCtx := context.WithoutCancel(ctx)
		if durableCancelled {
			_ = h.executions.Cancel(persistCtx, executionItem.ID, "execution cancelled")
		} else if ctx.Err() != nil {
			// The daemon is shutting down. Leave the durable execution RUNNING so
			// stale-job recovery can resume it on the next startup.
			return runErr
		} else {
			code, message, paused := classifyExecutorError(runErr)
			_ = h.executions.Fail(persistCtx, executionItem.ID, code, message)
			if paused {
				return h.pauseWorkflow(persistCtx, job, queueJob, code, message)
			}
		}
		if durableCancelled {
			return runErr
		}
		return h.failWorkflowOnLastAttempt(context.WithoutCancel(ctx), job, queueJob, runErr)
	}

	snapshot, err := gitstate.Capture(ctx, item.Path, policy.MaxPatchBytes)
	if errors.Is(err, gitstate.ErrPatchTooLarge) {
		return ErrSizeLimit
	}
	if err != nil {
		return err
	}
	if job.BaseCommitSHA != "" && snapshot.Head != job.BaseCommitSHA {
		return fmt.Errorf("workspace HEAD %s does not match workflow base commit %s", snapshot.Head, job.BaseCommitSHA)
	}
	select {
	case cancellation := <-cancellationErr:
		if cancellation != nil {
			if errors.Is(cancellation, context.Canceled) {
				_ = h.executions.Cancel(context.WithoutCancel(ctx), executionItem.ID, "execution cancelled")
				return cancellation
			}
			_ = h.executions.Fail(context.WithoutCancel(ctx), executionItem.ID, "EXECUTOR_FAILED", cancellation.Error())
			return h.failWorkflowOnLastAttempt(context.WithoutCancel(ctx), job, queueJob, cancellation)
		}
	default:
	}
	if err := runCtx.Err(); err != nil {
		if ctx.Err() != nil {
			return err
		}
		_ = h.executions.Fail(context.WithoutCancel(ctx), executionItem.ID, "EXECUTOR_FAILED", err.Error())
		return h.failWorkflowOnLastAttempt(context.WithoutCancel(ctx), job, queueJob, err)
	}
	artifactKey := ""
	if h.artifactStore != nil {
		artifactKey = fmt.Sprintf("workflows/%s/executions/%d/patch.diff", job.ID, executionItem.ExecutionVersion)
		if err := h.artifactStore.Save(ctx, artifactKey, strings.NewReader(snapshot.Patch)); err != nil {
			return fmt.Errorf("save execution patch artifact: %w", err)
		}
	}
	var checkpoint Checkpoint
	if artifactKey == "" {
		checkpoint, err = h.executions.SaveCheckpoint(
			ctx, executionItem.ID, job.BaseCommitSHA, snapshot.Head, snapshot.Patch, snapshot.ChangedFiles,
		)
	} else {
		checkpoint, err = h.executions.SaveCheckpointArtifact(
			ctx, executionItem.ID, job.BaseCommitSHA, snapshot.Head, snapshot.Patch, snapshot.ChangedFiles, artifactKey,
		)
	}
	if err != nil {
		return err
	}
	executionItem, err = h.executions.Complete(ctx, executionItem.ID)
	if err != nil {
		return err
	}
	if _, err := h.workspaces.ReleaseWrite(ctx, item.ID, lease.Token, snapshot.Head, snapshot.Dirty); err != nil {
		return err
	}
	released = true
	h.record(ctx, job.ID, "execution.completed", map[string]any{
		"execution_id":       executionItem.ID,
		"tool_calls":         executionItem.ToolCalls,
		"patch_checksum":     checkpoint.PatchChecksum,
		"changed_file_count": len(snapshot.ChangedFiles),
	}, time.Now().UTC())
	return h.finishWorkflow(ctx, job, executionItem, queueJob.ID)
}

func decodeExecutionDispatch(raw string) (dispatchPayload, error) {
	var payload dispatchPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, fmt.Errorf("decode workflow execute dispatch: %w", err)
	}
	if payload.WorkflowJobID == "" || payload.WorkflowVersion < 1 ||
		payload.TargetStatus != workflowjob.StatusQueued {
		return payload, fmt.Errorf("invalid workflow execute dispatch")
	}
	return payload, nil
}

func (h *Handler) ensureExecuting(
	ctx context.Context,
	job workflowjob.Job,
	payload dispatchPayload,
	dispatchID string,
) (workflowjob.Job, error) {
	if job.Status == workflowjob.StatusExecuting {
		return job, nil
	}
	if job.Version != payload.WorkflowVersion {
		return job, nil
	}
	updated, err := h.workflows.Transition(ctx, job.ID, workflowjob.TransitionInput{
		ExpectedVersion: job.Version,
		Action:          workflowjob.ActionExecutionStarted,
		Details:         map[string]any{"dispatch_job_id": dispatchID},
	})
	if err == nil {
		return updated, nil
	}
	if errors.Is(err, workflowjob.ErrVersionConflict) {
		current, getErr := h.workflows.Get(ctx, job.ID)
		if getErr == nil && current.Status == workflowjob.StatusExecuting {
			return current, nil
		}
	}
	return workflowjob.Job{}, err
}

func (h *Handler) finishWorkflow(
	ctx context.Context,
	job workflowjob.Job,
	executionItem Execution,
	dispatchID string,
) error {
	current, err := h.workflows.Get(ctx, job.ID)
	if err != nil {
		return err
	}
	if current.Status != workflowjob.StatusExecuting {
		return nil
	}
	_, err = h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
		ExpectedVersion: current.Version,
		Action:          workflowjob.ActionExecutionCompleted,
		Details: map[string]any{
			"execution_id":    executionItem.ID,
			"dispatch_job_id": dispatchID,
		},
	})
	if errors.Is(err, workflowjob.ErrVersionConflict) {
		latest, getErr := h.workflows.Get(ctx, current.ID)
		if getErr == nil && latest.Status != workflowjob.StatusExecuting {
			return nil
		}
	}
	return err
}

func (h *Handler) failWorkflowOnLastAttempt(
	ctx context.Context,
	job workflowjob.Job,
	queueJob jobqueue.Job,
	cause error,
) error {
	if queueJob.Attempts < queueJob.MaxAttempts {
		return cause
	}
	current, err := h.workflows.Get(ctx, job.ID)
	if err != nil {
		return cause
	}
	if current.Status == workflowjob.StatusExecuting {
		code, message, _ := classifyExecutorError(cause)
		_, _ = h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
			ExpectedVersion: current.Version,
			Action:          workflowjob.ActionFail,
			FailureCode:     code,
			FailureMessage:  message,
			Details: map[string]any{
				"attempt":      queueJob.Attempts,
				"max_attempts": queueJob.MaxAttempts,
			},
		})
	}
	return cause
}

func (h *Handler) renewLease(
	ctx context.Context,
	workspaceID string,
	token string,
	cancel context.CancelFunc,
	result chan<- error,
) {
	interval := h.leaseTTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := h.workspaces.Renew(
				context.WithoutCancel(ctx), workspaceID, token, h.leaseTTL,
			); err != nil {
				select {
				case result <- fmt.Errorf("renew workspace write lease: %w", err):
				default:
				}
				cancel()
				return
			}
		}
	}
}

func executorSnapshot(settings agentconfig.Settings) (
	agentconfig.AgentConfig,
	agentconfig.ToolPolicy,
	error,
) {
	var agent agentconfig.AgentConfig
	var policy agentconfig.ToolPolicy
	for _, candidate := range settings.Agents {
		if candidate.Role == agentconfig.RoleExecutor {
			agent = candidate
		}
	}
	for _, candidate := range settings.ToolPolicies {
		if candidate.Role == agentconfig.RoleExecutor {
			policy = candidate
		}
	}
	if agent.Role == "" || policy.Role == "" || !agent.Enabled {
		return agent, policy, fmt.Errorf("executor agent is not enabled")
	}
	return agent, policy, nil
}

func (h *Handler) record(ctx context.Context, jobID, event string, payload any, created time.Time) {
	if h.recorder != nil {
		_ = h.recorder.Record(context.WithoutCancel(ctx), jobID, event, payload, created)
	}
}

type RecordedTools struct {
	repository *Repository
	execution  Execution
	toolset    Toolset
	maxCalls   int
	mu         sync.Mutex
}

func (t *RecordedTools) invoke(
	ctx context.Context,
	tool string,
	input any,
	operation func() (any, error),
) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	run, err := t.repository.StartTool(ctx, t.execution.ID, tool, input, t.maxCalls)
	if err != nil {
		return nil, err
	}
	output, err := operation()
	if err != nil {
		_ = t.repository.FailTool(context.WithoutCancel(ctx), run.ID, "TOOL_FAILED", err.Error())
		return nil, err
	}
	if err := t.repository.CompleteTool(ctx, run.ID, output); err != nil {
		return nil, err
	}
	return output, nil
}

func (t *RecordedTools) GitStatus(ctx context.Context) (string, error) {
	value, err := t.invoke(ctx, agentconfig.ToolGitStatus, map[string]any{}, func() (any, error) {
		return t.toolset.GitStatus(ctx)
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

func (t *RecordedTools) GitDiff(ctx context.Context) (string, error) {
	value, err := t.invoke(ctx, agentconfig.ToolGitDiff, map[string]any{}, func() (any, error) {
		return t.toolset.GitDiff(ctx)
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

func (t *RecordedTools) Read(ctx context.Context, path string) (string, error) {
	value, err := t.invoke(ctx, agentconfig.ToolFileRead, map[string]any{"path": path}, func() (any, error) {
		return t.toolset.Read(path)
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

func (t *RecordedTools) Create(ctx context.Context, path, content string) error {
	_, err := t.invoke(ctx, agentconfig.ToolFileCreate, map[string]any{
		"path": path, "bytes": len([]byte(content)),
	}, func() (any, error) {
		return map[string]any{"changed": true}, t.toolset.Create(path, content)
	})
	return err
}

func (t *RecordedTools) Patch(ctx context.Context, path, expected, replacement string) error {
	_, err := t.invoke(ctx, agentconfig.ToolFilePatch, map[string]any{
		"path":        path,
		"patch_bytes": len([]byte(expected)) + len([]byte(replacement)),
	}, func() (any, error) {
		return map[string]any{"changed": true}, t.toolset.Patch(path, expected, replacement)
	})
	return err
}

func (t *RecordedTools) Delete(ctx context.Context, path string) error {
	_, err := t.invoke(ctx, agentconfig.ToolFileDelete, map[string]any{"path": path}, func() (any, error) {
		return map[string]any{"changed": true}, t.toolset.Delete(path)
	})
	return err
}

type ScriptedRunner struct{}

func (ScriptedRunner) Run(ctx context.Context, run RunContext) error {
	if _, err := run.Tools.GitStatus(ctx); err != nil {
		return err
	}
	_, err := run.Tools.GitDiff(ctx)
	return err
}

func (h *Handler) pauseWorkflow(
	ctx context.Context,
	job workflowjob.Job,
	queueJob jobqueue.Job,
	code string,
	message string,
) error {
	current, err := h.workflows.Get(ctx, job.ID)
	if err != nil {
		return err
	}
	if current.Status != workflowjob.StatusExecuting {
		return nil
	}
	updated, err := h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
		ExpectedVersion: current.Version,
		Action:          workflowjob.ActionFail,
		FailureCode:     code,
		FailureMessage:  message,
		Details: map[string]any{
			"paused":       true,
			"attempt":      queueJob.Attempts,
			"max_attempts": queueJob.MaxAttempts,
		},
	})
	if err != nil {
		return err
	}
	h.record(ctx, updated.ID, "execution.paused", map[string]any{
		"failure_code":       code,
		"failure_message":    message,
		"max_executor_turns": updated.Limits.MaxExecutorTurns,
	}, time.Now().UTC())
	return nil
}
