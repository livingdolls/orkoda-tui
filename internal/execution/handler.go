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
) (*Handler, error) {
	if workflows == nil || workspaces == nil || settings == nil || executions == nil || runner == nil {
		return nil, fmt.Errorf("workflow, workspace, settings, execution, and runner dependencies are required")
	}
	if strings.TrimSpace(workerID) == "" || leaseTTL <= 0 {
		return nil, fmt.Errorf("worker ID and positive write lease TTL are required")
	}
	return &Handler{workflows:workflows,workspaces:workspaces,settings:settings,executions:executions,runner:runner,recorder:recorder,workerID:workerID,leaseTTL:leaseTTL,defaultProvider:defaultProvider,defaultModel:defaultModel},nil
}

type dispatchPayload struct {
	WorkflowJobID  string             `json:"workflow_job_id"`
	WorkflowVersion int               `json:"workflow_version"`
	Action          workflowjob.Action `json:"action"`
	TargetStatus    workflowjob.Status `json:"target_status"`
}

func (h *Handler) HandleDurable(ctx context.Context, queueJob jobqueue.Job) error {
	var payload dispatchPayload
	if err:=json.Unmarshal([]byte(queueJob.PayloadJSON),&payload);err!=nil{return fmt.Errorf("decode workflow execute dispatch: %w",err)}
	if payload.WorkflowJobID==""||payload.WorkflowVersion<1||payload.TargetStatus!=workflowjob.StatusQueued{return fmt.Errorf("invalid workflow execute dispatch")}

	job,err:=h.workflows.Get(ctx,payload.WorkflowJobID);if err!=nil{return err}
	if job.Version<payload.WorkflowVersion{return fmt.Errorf("workflow version %d has not reached dispatch version %d",job.Version,payload.WorkflowVersion)}
	if job.Status!=workflowjob.StatusQueued&&job.Status!=workflowjob.StatusExecuting {
		return nil
	}
	if job.Status==workflowjob.StatusQueued {
		if job.Version!=payload.WorkflowVersion{return nil}
		job,err=h.workflows.Transition(ctx,job.ID,workflowjob.TransitionInput{ExpectedVersion:job.Version,Action:workflowjob.ActionExecutionStarted,Details:map[string]any{"dispatch_job_id":queueJob.ID}})
		if err!=nil {
			if errors.Is(err,workflowjob.ErrVersionConflict){current,getErr:=h.workflows.Get(ctx,job.ID);if getErr==nil&&current.Status==workflowjob.StatusExecuting{job=current}else{return err}} else {return err}
		}
	}
	if job.ExecutionVersion<1{return fmt.Errorf("workflow execution version is not initialized")}

	item,err:=h.workspaces.GetByWorkflow(ctx,job.ID);if err!=nil{return err}
	settings,err:=h.settings.Get(ctx,job.ProjectID);if err!=nil{return err}
	agent,policy,err:=executorSnapshot(settings);if err!=nil{return err}
	provider:=agent.Provider;if provider==""{provider=h.defaultProvider};model:=agent.Model;if model==""{model=h.defaultModel}
	execution,_,err:=h.executions.CreateOrGet(ctx,CreateInput{WorkflowJobID:job.ID,WorkflowVersion:job.Version,ExecutionVersion:job.ExecutionVersion,PlanVersionID:job.PlanVersionID,WorkspaceID:item.ID,BaseCommitSHA:job.BaseCommitSHA,AgentSettingsVersion:settings.Version,Provider:provider,Model:model});if err!=nil{return err}
	if execution.Status==StatusCompleted {
		return h.finishWorkflow(ctx,job,execution,queueJob)
	}
	if execution.Status==StatusFailed{return fmt.Errorf("execution %s is failed and requires workflow retry",execution.ID)}

	lease,err:=h.workspaces.AcquireWrite(ctx,item.ID,h.workerID,h.leaseTTL);if err!=nil{return err}
	released:=false
	defer func(){if !released{_ = h.workspaces.Release(ctx,item.ID,lease.Token)}}()

	runCtx,cancel:=context.WithCancel(ctx);defer cancel()
	leaseErr:=make(chan error,1)
	go h.renewLease(runCtx,item.ID,lease.Token,cancel,leaseErr)

	execution,err=h.executions.Start(ctx,execution.ID);if err!=nil{return err}
	tools:=&RecordedTools{repository:h.executions,execution:execution,toolset:Toolset{Root:item.Path,Policy:policy},maxCalls:job.Limits.MaxToolCalls}
	h.record(ctx,job.ID,"execution.started",map[string]any{"execution_id":execution.ID,"execution_version":execution.ExecutionVersion,"agent_settings_version":execution.AgentSettingsVersion},time.Now().UTC())

	runErr:=h.runner.Run(runCtx,RunContext{Execution:execution,Workspace:item,Tools:tools})
	select{case renewalErr:=<-leaseErr:if renewalErr!=nil{runErr=renewalErr};default:}
	if runErr!=nil {
		_ = h.executions.Fail(context.WithoutCancel(ctx),execution.ID,"EXECUTOR_FAILED",runErr.Error())
		return h.failWorkflowOnLastAttempt(context.WithoutCancel(ctx),job,queueJob,runErr)
	}

	patch,err:=tools.toolset.GitDiff(ctx);if err!=nil{return err}
	if policy.MaxPatchBytes>0&&len([]byte(patch))>policy.MaxPatchBytes{return ErrSizeLimit}
	changed,err:=tools.toolset.ChangedFiles(ctx);if err!=nil{return err}
	head,err:=tools.toolset.Head(ctx);if err!=nil{return err}
	checkpoint,err:=h.executions.SaveCheckpoint(ctx,execution.ID,job.BaseCommitSHA,head,patch,changed);if err!=nil{return err}
	execution,err=h.executions.Complete(ctx,execution.ID);if err!=nil{return err}
	if _,err:=h.workspaces.ReleaseWrite(ctx,item.ID,lease.Token,head,len(changed)>0);err!=nil{return err};released=true
	h.record(ctx,job.ID,"execution.completed",map[string]any{"execution_id":execution.ID,"tool_calls":execution.ToolCalls,"patch_checksum":checkpoint.PatchChecksum,"changed_file_count":len(changed)},time.Now().UTC())
	return h.finishWorkflow(ctx,job,execution,queueJob)
}

func (h *Handler) finishWorkflow(ctx context.Context, job workflowjob.Job, execution Execution, queueJob jobqueue.Job) error {
	current,err:=h.workflows.Get(ctx,job.ID);if err!=nil{return err}
	if current.Status!=workflowjob.StatusExecuting{return nil}
	_,err=h.workflows.Transition(ctx,current.ID,workflowjob.TransitionInput{ExpectedVersion:current.Version,Action:workflowjob.ActionExecutionCompleted,Details:map[string]any{"execution_id":execution.ID,"dispatch_job_id":queueJob.ID}})
	if errors.Is(err,workflowjob.ErrVersionConflict){latest,getErr:=h.workflows.Get(ctx,current.ID);if getErr==nil&&latest.Status!=workflowjob.StatusExecuting{return nil}}
	return err
}

func (h *Handler) failWorkflowOnLastAttempt(ctx context.Context, job workflowjob.Job, queueJob jobqueue.Job, cause error) error {
	if queueJob.Attempts<queueJob.MaxAttempts{return cause}
	current,err:=h.workflows.Get(ctx,job.ID);if err!=nil{return cause}
	if current.Status==workflowjob.StatusExecuting {
		_,_ = h.workflows.Transition(ctx,current.ID,workflowjob.TransitionInput{ExpectedVersion:current.Version,Action:workflowjob.ActionFail,FailureCode:"EXECUTION_FAILED",FailureMessage:cause.Error(),Details:map[string]any{"attempt":queueJob.Attempts,"max_attempts":queueJob.MaxAttempts}})
	}
	return cause
}

func (h *Handler) renewLease(ctx context.Context, workspaceID, token string, cancel context.CancelFunc, result chan<- error) {
	interval:=h.leaseTTL/3;if interval<time.Second{interval=time.Second};ticker:=time.NewTicker(interval);defer ticker.Stop()
	for{select{case<-ctx.Done():return;case<-ticker.C:if _,err:=h.workspaces.Renew(context.WithoutCancel(ctx),workspaceID,token,h.leaseTTL);err!=nil{select{case result<-fmt.Errorf("renew workspace write lease: %w",err):default:};cancel();return}}}
}

func executorSnapshot(settings agentconfig.Settings)(agentconfig.AgentConfig,agentconfig.ToolPolicy,error){var agent agentconfig.AgentConfig;var policy agentconfig.ToolPolicy;for _,candidate:=range settings.Agents{if candidate.Role==agentconfig.RoleExecutor{agent=candidate}};for _,candidate:=range settings.ToolPolicies{if candidate.Role==agentconfig.RoleExecutor{policy=candidate}};if agent.Role==""||policy.Role==""||!agent.Enabled{return agent,policy,fmt.Errorf("executor agent is not enabled")};return agent,policy,nil}
func (h *Handler) record(ctx context.Context,jobID,event string,payload any,created time.Time){if h.recorder!=nil{_ = h.recorder.Record(context.WithoutCancel(ctx),jobID,event,payload,created)}}

type RecordedTools struct {repository *Repository;execution Execution;toolset Toolset;maxCalls int;mu sync.Mutex}
func (t *RecordedTools) invoke(ctx context.Context,tool string,input any,operation func()(any,error))(any,error){t.mu.Lock();defer t.mu.Unlock();run,err:=t.repository.StartTool(ctx,t.execution.ID,tool,input,t.maxCalls);if err!=nil{return nil,err};output,err:=operation();if err!=nil{_ = t.repository.FailTool(context.WithoutCancel(ctx),run.ID,"TOOL_FAILED",err.Error());return nil,err};if err:=t.repository.CompleteTool(ctx,run.ID,output);err!=nil{return nil,err};return output,nil}
func (t *RecordedTools) GitStatus(ctx context.Context)(string,error){value,err:=t.invoke(ctx,agentconfig.ToolGitStatus,map[string]any{},func()(any,error){return t.toolset.GitStatus(ctx)});if err!=nil{return "",err};return value.(string),nil}
func (t *RecordedTools) GitDiff(ctx context.Context)(string,error){value,err:=t.invoke(ctx,agentconfig.ToolGitDiff,map[string]any{},func()(any,error){return t.toolset.GitDiff(ctx)});if err!=nil{return "",err};return value.(string),nil}
func (t *RecordedTools) Read(ctx context.Context,path string)(string,error){value,err:=t.invoke(ctx,agentconfig.ToolFileRead,map[string]any{"path":path},func()(any,error){return t.toolset.Read(path)});if err!=nil{return "",err};return value.(string),nil}
func (t *RecordedTools) Create(ctx context.Context,path,content string)error{_,err:=t.invoke(ctx,agentconfig.ToolFileCreate,map[string]any{"path":path,"bytes":len([]byte(content))},func()(any,error){return map[string]any{"changed":true},t.toolset.Create(path,content)});return err}
func (t *RecordedTools) Patch(ctx context.Context,path,expected,replacement string)error{_,err:=t.invoke(ctx,agentconfig.ToolFilePatch,map[string]any{"path":path,"patch_bytes":len([]byte(expected))+len([]byte(replacement))},func()(any,error){return map[string]any{"changed":true},t.toolset.Patch(path,expected,replacement)});return err}
func (t *RecordedTools) Delete(ctx context.Context,path string)error{_,err:=t.invoke(ctx,agentconfig.ToolFileDelete,map[string]any{"path":path},func()(any,error){return map[string]any{"changed":true},t.toolset.Delete(path)});return err}

type ScriptedRunner struct{}
func (ScriptedRunner) Run(ctx context.Context,run RunContext)error{_,err:=run.Tools.GitStatus(ctx);if err!=nil{return err};_,err=run.Tools.GitDiff(ctx);return err}
