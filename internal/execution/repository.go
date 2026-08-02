package execution

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound        = errors.New("execution not found")
	ErrInvalid         = errors.New("invalid execution")
	ErrToolCallLimit   = errors.New("execution tool-call limit reached")
	ErrDuplicateActive = errors.New("execution already active")
)

type Status string

const (
	StatusPending   Status = "PENDING"
	StatusRunning   Status = "RUNNING"
	StatusCompleted Status = "COMPLETED"
	StatusFailed    Status = "FAILED"
	StatusCancelled Status = "CANCELLED"
)

type Execution struct {
	ID                   string     `json:"id"`
	WorkflowJobID        string     `json:"workflow_job_id"`
	WorkflowVersion      int        `json:"workflow_version"`
	ExecutionVersion     int        `json:"execution_version"`
	PlanVersionID        string     `json:"plan_version_id"`
	WorkspaceID          string     `json:"workspace_id"`
	BaseCommitSHA        string     `json:"base_commit_sha"`
	AgentSettingsVersion int        `json:"agent_settings_version"`
	Provider             string     `json:"provider"`
	Model                string     `json:"model"`
	Status               Status     `json:"status"`
	ToolCalls            int        `json:"tool_calls"`
	FailureCode          string     `json:"failure_code,omitempty"`
	FailureMessage       string     `json:"failure_message,omitempty"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type ToolRun struct {
	ID                string          `json:"id"`
	ExecutionID       string          `json:"execution_id"`
	Sequence          int             `json:"sequence"`
	Tool              string          `json:"tool"`
	Status            Status          `json:"status"`
	InputSummaryJSON  json.RawMessage `json:"input_summary"`
	OutputSummaryJSON json.RawMessage `json:"output_summary"`
	ErrorCode         string          `json:"error_code,omitempty"`
	ErrorMessage      string          `json:"error_message,omitempty"`
	StartedAt         *time.Time      `json:"started_at,omitempty"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type Checkpoint struct {
	ID               string          `json:"id"`
	ExecutionID      string          `json:"execution_id"`
	Sequence         int             `json:"sequence"`
	BaseCommitSHA    string          `json:"base_commit_sha"`
	WorkspaceHeadSHA string          `json:"workspace_head_sha"`
	PatchChecksum    string          `json:"patch_checksum"`
	PatchBytes       int             `json:"patch_bytes"`
	ChangedFilesJSON json.RawMessage `json:"changed_files"`
	PatchText        string          `json:"-"`
	CreatedAt        time.Time       `json:"created_at"`
}

type CreateInput struct {
	WorkflowJobID        string
	WorkflowVersion      int
	ExecutionVersion     int
	PlanVersionID        string
	WorkspaceID          string
	BaseCommitSHA        string
	AgentSettingsVersion int
	Provider             string
	Model                string
}

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	return &Repository{db: db, now: time.Now}, nil
}

func (r *Repository) CreateOrGet(ctx context.Context, input CreateInput) (Execution, bool, error) {
	input.WorkflowJobID = strings.TrimSpace(input.WorkflowJobID)
	input.PlanVersionID = strings.TrimSpace(input.PlanVersionID)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.BaseCommitSHA = strings.TrimSpace(input.BaseCommitSHA)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Model = strings.TrimSpace(input.Model)
	if input.WorkflowJobID == "" || input.PlanVersionID == "" || input.WorkspaceID == "" ||
		input.BaseCommitSHA == "" || input.WorkflowVersion < 1 || input.ExecutionVersion < 1 ||
		input.AgentSettingsVersion < 1 {
		return Execution{}, false, fmt.Errorf("%w: required snapshot fields are missing", ErrInvalid)
	}

	if existing, err := r.GetByVersion(ctx, input.WorkflowJobID, input.ExecutionVersion); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Execution{}, false, err
	}

	now := r.now().UTC()
	item := Execution{
		ID: newID(), WorkflowJobID: input.WorkflowJobID, WorkflowVersion: input.WorkflowVersion,
		ExecutionVersion: input.ExecutionVersion, PlanVersionID: input.PlanVersionID,
		WorkspaceID: input.WorkspaceID, BaseCommitSHA: input.BaseCommitSHA,
		AgentSettingsVersion: input.AgentSettingsVersion, Provider: input.Provider,
		Model: input.Model, Status: StatusPending, CreatedAt: now, UpdatedAt: now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO executions (
			id, workflow_job_id, workflow_version, execution_version,
			plan_version_id, workspace_id, base_commit_sha,
			agent_settings_version, provider, model, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.WorkflowJobID, item.WorkflowVersion, item.ExecutionVersion,
		item.PlanVersionID, item.WorkspaceID, item.BaseCommitSHA,
		item.AgentSettingsVersion, item.Provider, item.Model, item.Status,
		now.UnixMilli(), now.UnixMilli())
	if err != nil {
		if existing, getErr := r.GetByVersion(ctx, input.WorkflowJobID, input.ExecutionVersion); getErr == nil {
			return existing, false, nil
		}
		return Execution{}, false, fmt.Errorf("insert execution: %w", err)
	}
	return item, true, nil
}

func (r *Repository) Get(ctx context.Context, executionID string) (Execution, error) {
	return loadExecution(r.db.QueryRowContext(ctx, `SELECT `+executionColumns+` FROM executions WHERE id = ?`, strings.TrimSpace(executionID)))
}

func (r *Repository) GetByVersion(ctx context.Context, workflowID string, version int) (Execution, error) {
	return loadExecution(r.db.QueryRowContext(ctx, `SELECT `+executionColumns+` FROM executions WHERE workflow_job_id = ? AND execution_version = ?`, strings.TrimSpace(workflowID), version))
}

func (r *Repository) ListWorkflow(ctx context.Context, workflowID string) ([]Execution, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+executionColumns+` FROM executions WHERE workflow_job_id = ? ORDER BY execution_version DESC`, strings.TrimSpace(workflowID))
	if err != nil { return nil, fmt.Errorf("list executions: %w", err) }
	defer rows.Close()
	items := make([]Execution, 0)
	for rows.Next() { item, err := scanExecution(rows); if err != nil { return nil, err }; items = append(items, item) }
	return items, rows.Err()
}

func (r *Repository) Start(ctx context.Context, executionID string) (Execution, error) {
	now := r.now().UTC()
	row := r.db.QueryRowContext(ctx, `UPDATE executions SET status='RUNNING', started_at=COALESCE(started_at, ?), updated_at=? WHERE id=? AND status IN ('PENDING','RUNNING') RETURNING `+executionColumns, now.UnixMilli(), now.UnixMilli(), executionID)
	return loadExecution(row)
}

func (r *Repository) Complete(ctx context.Context, executionID string) (Execution, error) {
	now := r.now().UTC()
	row := r.db.QueryRowContext(ctx, `UPDATE executions SET status='COMPLETED', completed_at=?, updated_at=?, failure_code=NULL, failure_message=NULL WHERE id=? AND status IN ('PENDING','RUNNING','COMPLETED') RETURNING `+executionColumns, now.UnixMilli(), now.UnixMilli(), executionID)
	return loadExecution(row)
}

func (r *Repository) Fail(ctx context.Context, executionID, code, message string) error {
	if len(message) > 2048 { message = message[:2048] }
	now := r.now().UTC()
	_, err := r.db.ExecContext(ctx, `UPDATE executions SET status='FAILED', failure_code=?, failure_message=?, completed_at=?, updated_at=? WHERE id=?`, nullable(code), nullable(message), now.UnixMilli(), now.UnixMilli(), executionID)
	return err
}

func (r *Repository) StartTool(ctx context.Context, executionID, tool string, input any, maxCalls int) (ToolRun, error) {
	inputJSON, err := json.Marshal(input); if err != nil { return ToolRun{}, fmt.Errorf("marshal tool input summary: %w", err) }
	now := r.now().UTC()
	tx, err := r.db.BeginTx(ctx, nil); if err != nil { return ToolRun{}, err }; defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT tool_calls FROM executions WHERE id=?`, executionID).Scan(&count); errors.Is(err, sql.ErrNoRows) { return ToolRun{}, ErrNotFound } else if err != nil { return ToolRun{}, err }
	if maxCalls > 0 && count >= maxCalls { return ToolRun{}, ErrToolCallLimit }
	sequence := count + 1
	item := ToolRun{ID:newID(), ExecutionID:executionID, Sequence:sequence, Tool:tool, Status:StatusRunning, InputSummaryJSON:inputJSON, OutputSummaryJSON:json.RawMessage(`{}`), StartedAt:&now, CreatedAt:now, UpdatedAt:now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tool_runs (id,execution_id,sequence,tool,status,input_summary_json,output_summary_json,started_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, item.ID,item.ExecutionID,item.Sequence,item.Tool,item.Status,string(inputJSON),`{}`,now.UnixMilli(),now.UnixMilli(),now.UnixMilli()); err != nil { return ToolRun{}, err }
	if _, err := tx.ExecContext(ctx, `UPDATE executions SET tool_calls=?, updated_at=? WHERE id=?`, sequence, now.UnixMilli(), executionID); err != nil { return ToolRun{}, err }
	if err := tx.Commit(); err != nil { return ToolRun{}, err }
	return item,nil
}

func (r *Repository) CompleteTool(ctx context.Context, toolRunID string, output any) error {
	payload, err := json.Marshal(output); if err != nil { return err }
	now := r.now().UTC()
	_, err = r.db.ExecContext(ctx, `UPDATE tool_runs SET status='COMPLETED', output_summary_json=?, completed_at=?, updated_at=? WHERE id=?`, string(payload), now.UnixMilli(), now.UnixMilli(), toolRunID)
	return err
}

func (r *Repository) FailTool(ctx context.Context, toolRunID, code, message string) error {
	if len(message)>1024 { message=message[:1024] }
	now:=r.now().UTC()
	_,err:=r.db.ExecContext(ctx,`UPDATE tool_runs SET status='FAILED',error_code=?,error_message=?,completed_at=?,updated_at=? WHERE id=?`,nullable(code),nullable(message),now.UnixMilli(),now.UnixMilli(),toolRunID)
	return err
}

func (r *Repository) ListToolRuns(ctx context.Context, executionID string) ([]ToolRun,error) {
	rows,err:=r.db.QueryContext(ctx,`SELECT `+toolRunColumns+` FROM tool_runs WHERE execution_id=? ORDER BY sequence`,executionID); if err!=nil{return nil,err}; defer rows.Close()
	items:=make([]ToolRun,0); for rows.Next(){item,err:=scanToolRun(rows);if err!=nil{return nil,err};items=append(items,item)};return items,rows.Err()
}

func (r *Repository) SaveCheckpoint(ctx context.Context, executionID, baseSHA, headSHA, patch string, changedFiles []string) (Checkpoint,error) {
	changedJSON,err:=json.Marshal(changedFiles);if err!=nil{return Checkpoint{},err}
	hash:=sha256.Sum256([]byte(patch)); checksum:="sha256:"+hex.EncodeToString(hash[:])
	var sequence int
	if err:=r.db.QueryRowContext(ctx,`SELECT COALESCE(MAX(sequence),0)+1 FROM patch_checkpoints WHERE execution_id=?`,executionID).Scan(&sequence);err!=nil{return Checkpoint{},err}
	now:=r.now().UTC(); item:=Checkpoint{ID:newID(),ExecutionID:executionID,Sequence:sequence,BaseCommitSHA:baseSHA,WorkspaceHeadSHA:headSHA,PatchChecksum:checksum,PatchBytes:len([]byte(patch)),ChangedFilesJSON:changedJSON,PatchText:patch,CreatedAt:now}
	_,err=r.db.ExecContext(ctx,`INSERT INTO patch_checkpoints (id,execution_id,sequence,base_commit_sha,workspace_head_sha,patch_checksum,patch_bytes,changed_files_json,patch_text,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,item.ID,item.ExecutionID,item.Sequence,item.BaseCommitSHA,item.WorkspaceHeadSHA,item.PatchChecksum,item.PatchBytes,string(changedJSON),item.PatchText,now.UnixMilli())
	if err!=nil { if existing,getErr:=r.latestCheckpoint(ctx,executionID);getErr==nil&&existing.PatchChecksum==checksum{return existing,nil};return Checkpoint{},err }
	return item,nil
}

func (r *Repository) ListCheckpoints(ctx context.Context, executionID string)([]Checkpoint,error){rows,err:=r.db.QueryContext(ctx,`SELECT id,execution_id,sequence,base_commit_sha,workspace_head_sha,patch_checksum,patch_bytes,changed_files_json,patch_text,created_at FROM patch_checkpoints WHERE execution_id=? ORDER BY sequence`,executionID);if err!=nil{return nil,err};defer rows.Close();items:=make([]Checkpoint,0);for rows.Next(){var item Checkpoint;var created int64;if err:=rows.Scan(&item.ID,&item.ExecutionID,&item.Sequence,&item.BaseCommitSHA,&item.WorkspaceHeadSHA,&item.PatchChecksum,&item.PatchBytes,&item.ChangedFilesJSON,&item.PatchText,&created);err!=nil{return nil,err};item.CreatedAt=time.UnixMilli(created).UTC();items=append(items,item)};return items,rows.Err()}

func (r *Repository) latestCheckpoint(ctx context.Context, executionID string)(Checkpoint,error){items,err:=r.ListCheckpoints(ctx,executionID);if err!=nil||len(items)==0{return Checkpoint{},ErrNotFound};return items[len(items)-1],nil}

const executionColumns=`id,workflow_job_id,workflow_version,execution_version,plan_version_id,workspace_id,base_commit_sha,agent_settings_version,provider,model,status,tool_calls,failure_code,failure_message,started_at,completed_at,created_at,updated_at`
const toolRunColumns=`id,execution_id,sequence,tool,status,input_summary_json,output_summary_json,error_code,error_message,started_at,completed_at,created_at,updated_at`

func loadExecution(row interface{Scan(...any)error})(Execution,error){item,err:=scanExecution(row);if errors.Is(err,sql.ErrNoRows){return Execution{},ErrNotFound};return item,err}
func scanExecution(row interface{Scan(...any)error})(Execution,error){var item Execution;var failureCode,failureMessage sql.NullString;var started,completed sql.NullInt64;var created,updated int64;err:=row.Scan(&item.ID,&item.WorkflowJobID,&item.WorkflowVersion,&item.ExecutionVersion,&item.PlanVersionID,&item.WorkspaceID,&item.BaseCommitSHA,&item.AgentSettingsVersion,&item.Provider,&item.Model,&item.Status,&item.ToolCalls,&failureCode,&failureMessage,&started,&completed,&created,&updated);if err!=nil{return Execution{},err};item.FailureCode=failureCode.String;item.FailureMessage=failureMessage.String;item.CreatedAt=time.UnixMilli(created).UTC();item.UpdatedAt=time.UnixMilli(updated).UTC();if started.Valid{v:=time.UnixMilli(started.Int64).UTC();item.StartedAt=&v};if completed.Valid{v:=time.UnixMilli(completed.Int64).UTC();item.CompletedAt=&v};return item,nil}
func scanToolRun(row interface{Scan(...any)error})(ToolRun,error){var item ToolRun;var code,message sql.NullString;var started,completed sql.NullInt64;var created,updated int64;err:=row.Scan(&item.ID,&item.ExecutionID,&item.Sequence,&item.Tool,&item.Status,&item.InputSummaryJSON,&item.OutputSummaryJSON,&code,&message,&started,&completed,&created,&updated);if err!=nil{return ToolRun{},err};item.ErrorCode=code.String;item.ErrorMessage=message.String;item.CreatedAt=time.UnixMilli(created).UTC();item.UpdatedAt=time.UnixMilli(updated).UTC();if started.Valid{v:=time.UnixMilli(started.Int64).UTC();item.StartedAt=&v};if completed.Valid{v:=time.UnixMilli(completed.Int64).UTC();item.CompletedAt=&v};return item,nil}
func nullable(value string)any{if strings.TrimSpace(value)==""{return nil};return value}
func newID()string{var value[16]byte;if _,err:=rand.Read(value[:]);err!=nil{panic(err)};return hex.EncodeToString(value[:])}
