package reviewer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
	"github.com/livingdolls/orkoda-tui/internal/checks"
	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type WorkflowStore interface {
	Get(context.Context, string) (workflowjob.Job, error)
	Transition(context.Context, string, workflowjob.TransitionInput) (workflowjob.Job, error)
}

type ExecutionStore interface {
	GetByVersion(context.Context, string, int) (execution.Execution, error)
	ListCheckpoints(context.Context, string) ([]execution.Checkpoint, error)
}

type CheckStore interface {
	GetByVersion(context.Context, string, int) (checks.Run, error)
	ListSteps(context.Context, string) ([]checks.Step, error)
}

type SettingsStore interface {
	Get(context.Context, string) (agentconfig.Settings, error)
}

type ContextSource interface {
	Build(context.Context, string, execution.Execution, execution.Checkpoint, checks.Run, []checks.Step) (Context, ValidationContext, error)
}

type CompletionGateway interface {
	Complete(context.Context, string, llm.Request) (llm.Response, error)
}

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type Handler struct {
	workflows       WorkflowStore
	executions      ExecutionStore
	checks          CheckStore
	settings        SettingsStore
	reviews         Store
	contexts        ContextSource
	gateway         CompletionGateway
	recorder        EventRecorder
	defaultProvider string
	defaultModel    string
}

func NewHandler(
	workflows WorkflowStore,
	executions ExecutionStore,
	checkStore CheckStore,
	settings SettingsStore,
	reviews Store,
	contexts ContextSource,
	gateway CompletionGateway,
	recorder EventRecorder,
	defaultProvider string,
	defaultModel string,
) (*Handler, error) {
	defaultProvider = strings.TrimSpace(defaultProvider)
	defaultModel = strings.TrimSpace(defaultModel)
	if workflows == nil || executions == nil || checkStore == nil || settings == nil ||
		reviews == nil || contexts == nil || gateway == nil || defaultProvider == "" || defaultModel == "" {
		return nil, fmt.Errorf("reviewer workflow, execution, checks, settings, persistence, context, gateway, and defaults are required")
	}
	return &Handler{
		workflows:       workflows,
		executions:      executions,
		checks:          checkStore,
		settings:        settings,
		reviews:         reviews,
		contexts:        contexts,
		gateway:         gateway,
		recorder:        recorder,
		defaultProvider: defaultProvider,
		defaultModel:    defaultModel,
	}, nil
}

type dispatchPayload struct {
	WorkflowJobID   string             `json:"workflow_job_id"`
	WorkflowVersion int                `json:"workflow_version"`
	Action          workflowjob.Action `json:"action"`
	TargetStatus    workflowjob.Status `json:"target_status"`
}

func (h *Handler) HandleDurable(ctx context.Context, queueJob jobqueue.Job) error {
	payload, err := decodeDispatch(queueJob.PayloadJSON)
	if err != nil {
		return err
	}
	runID, err := h.handle(ctx, queueJob, payload)
	if err == nil {
		return nil
	}
	cancelled := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	if runID != "" {
		code, message := safeFailure(err)
		_ = h.reviews.Fail(context.WithoutCancel(ctx), runID, code, message, cancelled)
	}
	if queueJob.Attempts >= queueJob.MaxAttempts && !cancelled {
		h.failWorkflow(context.WithoutCancel(ctx), payload.WorkflowJobID, queueJob.ID, err)
	}
	return err
}

func (h *Handler) handle(
	ctx context.Context,
	queueJob jobqueue.Job,
	payload dispatchPayload,
) (string, error) {
	job, err := h.workflows.Get(ctx, payload.WorkflowJobID)
	if err != nil {
		return "", err
	}
	if job.Version < payload.WorkflowVersion {
		return "", fmt.Errorf(
			"workflow version %d has not reached reviewer dispatch version %d",
			job.Version,
			payload.WorkflowVersion,
		)
	}
	if job.Status != workflowjob.StatusReviewing {
		return "", nil
	}
	if job.ExecutionVersion < 1 {
		return "", fmt.Errorf("workflow execution version is not initialized")
	}

	executionItem, err := h.executions.GetByVersion(ctx, job.ID, job.ExecutionVersion)
	if err != nil {
		return "", err
	}
	if executionItem.Status != execution.StatusCompleted {
		return "", fmt.Errorf("execution %s is not completed", executionItem.ID)
	}
	checkRun, err := h.checks.GetByVersion(ctx, job.ID, job.ExecutionVersion)
	if err != nil {
		return "", err
	}
	if checkRun.Status != checks.StatusPassed && checkRun.Status != checks.StatusFailed {
		return "", fmt.Errorf("check run %s is not completed", checkRun.ID)
	}
	checkSteps, err := h.checks.ListSteps(ctx, checkRun.ID)
	if err != nil {
		return "", err
	}
	checkpoints, err := h.executions.ListCheckpoints(ctx, executionItem.ID)
	if err != nil {
		return "", err
	}
	if len(checkpoints) == 0 {
		return "", fmt.Errorf("execution %s has no patch checkpoint", executionItem.ID)
	}
	checkpoint := checkpoints[len(checkpoints)-1]

	settings, err := h.settings.Get(ctx, job.ProjectID)
	if err != nil {
		return "", err
	}
	reviewerConfig, err := resolveReviewerConfig(settings, h.defaultProvider, h.defaultModel)
	if err != nil {
		return "", err
	}
	run, _, err := h.reviews.CreateOrGet(ctx, CreateInput{
		WorkflowJobID:        job.ID,
		ExecutionID:          executionItem.ID,
		ExecutionVersion:     job.ExecutionVersion,
		CheckRunID:           checkRun.ID,
		CheckpointID:         checkpoint.ID,
		AgentSettingsVersion: settings.Version,
		Provider:             reviewerConfig.Provider,
		Model:                reviewerConfig.Model,
	})
	if err != nil {
		return "", err
	}
	if run.Status == StatusCompleted {
		return run.ID, h.finishWorkflow(ctx, job, run, queueJob.ID)
	}
	run, err = h.reviews.Start(ctx, run.ID)
	if err != nil {
		return run.ID, err
	}

	reviewContext, validation, err := h.contexts.Build(
		ctx,
		job.PlanVersionID,
		executionItem,
		checkpoint,
		checkRun,
		checkSteps,
	)
	if err != nil {
		return run.ID, err
	}
	request, err := BuildRequest(reviewContext, RequestConfig{
		RunID:             run.ID,
		WorkflowJobID:     job.ID,
		Model:             reviewerConfig.Model,
		Temperature:       reviewerConfig.Temperature,
		MaxOutputTokens:   reviewerConfig.MaxOutputTokens,
		SystemInstruction: reviewerConfig.SystemInstruction,
	})
	if err != nil {
		return run.ID, err
	}
	h.record(ctx, job.ID, "review.started", map[string]any{
		"review_run_id":    run.ID,
		"execution_id":     executionItem.ID,
		"check_run_id":     checkRun.ID,
		"checkpoint_id":    checkpoint.ID,
		"provider":         reviewerConfig.Provider,
		"model":            reviewerConfig.Model,
		"changed_file_count": len(reviewContext.ChangedFiles),
	}, time.Now().UTC())
	response, err := h.gateway.Complete(ctx, reviewerConfig.Provider, request)
	if err != nil {
		return run.ID, err
	}
	result, err := ParseResponse(response, validation)
	if err != nil {
		return run.ID, err
	}
	run, err = h.reviews.Complete(context.WithoutCancel(ctx), run.ID, result, response.Usage)
	if err != nil {
		return run.ID, err
	}
	h.record(ctx, job.ID, "review.completed", map[string]any{
		"review_run_id":  run.ID,
		"verdict":        run.Verdict,
		"total_issues":   run.TotalIssues,
		"blocking_issues": run.BlockingIssues,
		"input_tokens":   response.Usage.InputTokens,
		"output_tokens":  response.Usage.OutputTokens,
	}, time.Now().UTC())
	return run.ID, h.finishWorkflow(ctx, job, run, queueJob.ID)
}

func decodeDispatch(raw string) (dispatchPayload, error) {
	var payload dispatchPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, fmt.Errorf("decode reviewer dispatch: %w", err)
	}
	payload.WorkflowJobID = strings.TrimSpace(payload.WorkflowJobID)
	if payload.WorkflowJobID == "" || payload.WorkflowVersion < 1 ||
		payload.Action != workflowjob.ActionChecksCompleted ||
		payload.TargetStatus != workflowjob.StatusReviewing {
		return payload, fmt.Errorf("invalid reviewer dispatch")
	}
	return payload, nil
}

func resolveReviewerConfig(
	settings agentconfig.Settings,
	defaultProvider string,
	defaultModel string,
) (agentconfig.AgentConfig, error) {
	for _, config := range settings.Agents {
		if config.Role != agentconfig.RoleReviewer {
			continue
		}
		if !config.Enabled {
			return agentconfig.AgentConfig{}, fmt.Errorf("reviewer agent is disabled")
		}
		if strings.TrimSpace(config.Provider) == "" {
			config.Provider = defaultProvider
		}
		if strings.TrimSpace(config.Model) == "" {
			config.Model = defaultModel
		}
		if config.MaxOutputTokens <= 0 {
			config.MaxOutputTokens = 4096
		}
		return config, nil
	}
	return agentconfig.AgentConfig{}, fmt.Errorf("reviewer agent configuration is missing")
}

func (h *Handler) finishWorkflow(
	ctx context.Context,
	job workflowjob.Job,
	run Run,
	dispatchID string,
) error {
	current, err := h.workflows.Get(ctx, job.ID)
	if err != nil {
		return err
	}
	if current.Status != workflowjob.StatusReviewing {
		return nil
	}
	_, err = h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
		ExpectedVersion: current.Version,
		Action:          workflowjob.ActionReviewCompleted,
		Details: map[string]any{
			"review_run_id":    run.ID,
			"verdict":          run.Verdict,
			"total_issues":     run.TotalIssues,
			"blocking_issues":  run.BlockingIssues,
			"execution_version": run.ExecutionVersion,
			"checkpoint_id":    run.CheckpointID,
			"dispatch_job_id":  dispatchID,
		},
	})
	if errors.Is(err, workflowjob.ErrVersionConflict) {
		latest, getErr := h.workflows.Get(ctx, current.ID)
		if getErr == nil && latest.Status != workflowjob.StatusReviewing {
			return nil
		}
	}
	return err
}

func (h *Handler) failWorkflow(ctx context.Context, workflowID string, dispatchID string, cause error) {
	current, err := h.workflows.Get(ctx, workflowID)
	if err != nil || current.Status != workflowjob.StatusReviewing {
		return
	}
	_, message := safeFailure(cause)
	_, _ = h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
		ExpectedVersion: current.Version,
		Action:          workflowjob.ActionFail,
		FailureCode:     "REVIEWER_HANDLER_FAILED",
		FailureMessage:  message,
		Details: map[string]any{
			"dispatch_job_id": dispatchID,
		},
	})
}

func safeFailure(cause error) (string, string) {
	code := "REVIEWER_FAILED"
	message := "reviewer agent failed"
	if providerError, ok := llm.AsProviderError(cause); ok {
		code = string(providerError.Code)
		message = strings.TrimSpace(providerError.Message)
	} else if cause != nil {
		message = strings.TrimSpace(cause.Error())
	}
	if len(message) > 1024 {
		message = message[:1024]
	}
	return code, message
}

func (h *Handler) record(
	ctx context.Context,
	jobID string,
	event string,
	payload any,
	created time.Time,
) {
	if h.recorder != nil {
		_ = h.recorder.Record(context.WithoutCancel(ctx), jobID, event, payload, created)
	}
}
