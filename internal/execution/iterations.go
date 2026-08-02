package execution

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/llm"
)

type Iteration struct {
	ID                string          `json:"id"`
	ExecutionID       string          `json:"execution_id"`
	Sequence          int             `json:"sequence"`
	Provider          string          `json:"provider"`
	Model             string          `json:"model"`
	Status            Status          `json:"status"`
	ActionType        string          `json:"action_type"`
	Tool              string          `json:"tool,omitempty"`
	ActionSummaryJSON json.RawMessage `json:"action_summary"`
	ResultSummaryJSON json.RawMessage `json:"result_summary"`
	UsageJSON         json.RawMessage `json:"usage"`
	ErrorCode         string          `json:"error_code,omitempty"`
	ErrorMessage      string          `json:"error_message,omitempty"`
	StartedAt         time.Time       `json:"started_at"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type IterationInput struct {
	Provider      string
	Model         string
	ActionType    string
	Tool          string
	ActionSummary any
	Usage         llm.Usage
}

func (r *Repository) BeginIteration(ctx context.Context, executionID string, input IterationInput) (Iteration, error) {
	executionID = strings.TrimSpace(executionID)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Model = strings.TrimSpace(input.Model)
	input.ActionType = strings.TrimSpace(input.ActionType)
	input.Tool = strings.TrimSpace(input.Tool)
	if executionID == "" || input.Provider == "" || input.Model == "" ||
		(input.ActionType != "tool" && input.ActionType != "finish") {
		return Iteration{}, fmt.Errorf("%w: invalid executor iteration", ErrInvalid)
	}
	if input.ActionType == "tool" && input.Tool == "" {
		return Iteration{}, fmt.Errorf("%w: tool action requires a tool", ErrInvalid)
	}
	actionJSON, err := json.Marshal(input.ActionSummary)
	if err != nil {
		return Iteration{}, fmt.Errorf("marshal iteration action summary: %w", err)
	}
	usageJSON, err := json.Marshal(input.Usage)
	if err != nil {
		return Iteration{}, fmt.Errorf("marshal iteration usage: %w", err)
	}

	now := r.now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Iteration{}, err
	}
	defer tx.Rollback()
	var sequence int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM executor_iterations WHERE execution_id = ?`, executionID).Scan(&sequence); err != nil {
		return Iteration{}, err
	}
	item := Iteration{
		ID: newIterationID(), ExecutionID: executionID, Sequence: sequence,
		Provider: input.Provider, Model: input.Model, Status: StatusRunning,
		ActionType: input.ActionType, Tool: input.Tool,
		ActionSummaryJSON: actionJSON, ResultSummaryJSON: json.RawMessage(`{}`),
		UsageJSON: usageJSON, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO executor_iterations (
			id, execution_id, sequence, provider, model, status, action_type, tool,
			action_summary_json, result_summary_json, usage_json,
			started_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 'RUNNING', ?, ?, ?, '{}', ?, ?, ?, ?)
	`, item.ID, item.ExecutionID, item.Sequence, item.Provider, item.Model,
		item.ActionType, nullableIteration(item.Tool), string(actionJSON), string(usageJSON),
		now.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return Iteration{}, fmt.Errorf("insert executor iteration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Iteration{}, err
	}
	return item, nil
}

func (r *Repository) CompleteIteration(ctx context.Context, iterationID string, result any) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal iteration result summary: %w", err)
	}
	now := r.now().UTC()
	resultSQL, err := r.db.ExecContext(ctx, `
		UPDATE executor_iterations
		SET status = 'COMPLETED', result_summary_json = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND status = 'RUNNING'
	`, string(payload), now.UnixMilli(), now.UnixMilli(), strings.TrimSpace(iterationID))
	if err != nil {
		return err
	}
	return requireIteration(resultSQL)
}

func (r *Repository) FailIteration(ctx context.Context, iterationID, code, message string) error {
	message = strings.TrimSpace(message)
	if len(message) > 1024 {
		message = message[:1024]
	}
	now := r.now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE executor_iterations
		SET status = 'FAILED', error_code = ?, error_message = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND status = 'RUNNING'
	`, nullableIteration(strings.TrimSpace(code)), nullableIteration(message),
		now.UnixMilli(), now.UnixMilli(), strings.TrimSpace(iterationID))
	if err != nil {
		return err
	}
	return requireIteration(result)
}

func (r *Repository) ListIterations(ctx context.Context, executionID string) ([]Iteration, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, execution_id, sequence, provider, model, status, action_type, tool,
			action_summary_json, result_summary_json, usage_json,
			error_code, error_message, started_at, completed_at, created_at, updated_at
		FROM executor_iterations WHERE execution_id = ? ORDER BY sequence
	`, strings.TrimSpace(executionID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Iteration, 0)
	for rows.Next() {
		var item Iteration
		var tool, errorCode, errorMessage sql.NullString
		var actionSummary, resultSummary, usage string
		var completedAt sql.NullInt64
		var startedAt, createdAt, updatedAt int64
		if err := rows.Scan(
			&item.ID, &item.ExecutionID, &item.Sequence, &item.Provider, &item.Model,
			&item.Status, &item.ActionType, &tool, &actionSummary,
			&resultSummary, &usage, &errorCode, &errorMessage,
			&startedAt, &completedAt, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		item.ActionSummaryJSON = json.RawMessage(actionSummary)
		item.ResultSummaryJSON = json.RawMessage(resultSummary)
		item.UsageJSON = json.RawMessage(usage)
		item.Tool = tool.String
		item.ErrorCode = errorCode.String
		item.ErrorMessage = errorMessage.String
		item.StartedAt = time.UnixMilli(startedAt).UTC()
		item.CreatedAt = time.UnixMilli(createdAt).UTC()
		item.UpdatedAt = time.UnixMilli(updatedAt).UTC()
		if completedAt.Valid {
			value := time.UnixMilli(completedAt.Int64).UTC()
			item.CompletedAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) RecoverRunningIterations(ctx context.Context, executionID string) error {
	now := r.now().UTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE executor_iterations
		SET status = 'FAILED', error_code = 'INTERRUPTED',
			error_message = 'executor iteration was interrupted before completion',
			completed_at = ?, updated_at = ?
		WHERE execution_id = ? AND status = 'RUNNING'
	`, now.UnixMilli(), now.UnixMilli(), strings.TrimSpace(executionID))
	return err
}

func requireIteration(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func nullableIteration(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func newIterationID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate executor iteration ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}

var _ = errors.Is
