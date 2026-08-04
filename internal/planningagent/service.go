package planningagent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
	"github.com/livingdolls/orkoda-tui/internal/plans"
)

var (
	ErrRunNotFound         = errors.New("planning run not found")
	ErrActiveRun           = errors.New("plan already has an active planning run")
	ErrRunNotAwaitingInput = errors.New("planning run is not awaiting input")
	ErrInvalidAnswers      = errors.New("invalid planning answers")
	ErrStaleRun            = errors.New("planning run no longer matches the current plan context")
)

type RunStatus string

const (
	RunStatusRunning    RunStatus = "RUNNING"
	RunStatusNeedsInput RunStatus = "NEEDS_INPUT"
	RunStatusCompleted  RunStatus = "COMPLETED"
	RunStatusFailed     RunStatus = "FAILED"
	RunStatusCancelled  RunStatus = "CANCELLED"
	RunStatusSuperseded RunStatus = "SUPERSEDED"
)

type QuestionStatus string

const (
	QuestionStatusOpen     QuestionStatus = "OPEN"
	QuestionStatusAnswered QuestionStatus = "ANSWERED"
)

type Question struct {
	ID         string         `json:"id"`
	RunID      string         `json:"run_id"`
	Position   int            `json:"position"`
	Question   string         `json:"question"`
	Answer     string         `json:"answer,omitempty"`
	Status     QuestionStatus `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	AnsweredAt *time.Time     `json:"answered_at,omitempty"`
}

type Run struct {
	ID                string        `json:"id"`
	PlanID            string        `json:"plan_id"`
	PlanVersionID     string        `json:"plan_version_id"`
	PlanningContextID string        `json:"planning_context_id"`
	ParentRunID       string        `json:"parent_run_id,omitempty"`
	Provider          string        `json:"provider"`
	Model             string        `json:"model"`
	Status            RunStatus     `json:"status"`
	Result            *Plan         `json:"result,omitempty"`
	Questions         []Question    `json:"questions"`
	Usage             llm.Usage     `json:"usage"`
	ErrorCode         llm.ErrorCode `json:"error_code,omitempty"`
	ErrorMessage      string        `json:"error_message,omitempty"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

type AnswerInput struct {
	QuestionID string `json:"question_id"`
	Answer     string `json:"answer"`
}

type ContextReader interface {
	Current(context.Context, string) (planningcontext.Context, error)
}

type PlanStore interface {
	Get(context.Context, string) (plans.Plan, error)
	Update(context.Context, string, string, plans.Status) (plans.Plan, error)
}

type CompletionGateway interface {
	Complete(context.Context, string, llm.Request) (llm.Response, error)
}

type PlanningEventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type Service struct {
	db       *sql.DB
	contexts ContextReader
	plans    PlanStore
	gateway  CompletionGateway
	recorder PlanningEventRecorder
}

func NewService(
	db *sql.DB,
	contexts ContextReader,
	planStore PlanStore,
	gateway CompletionGateway,
	recorder PlanningEventRecorder,
) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if contexts == nil {
		return nil, fmt.Errorf("planning context reader is required")
	}
	if planStore == nil {
		return nil, fmt.Errorf("plan store is required")
	}
	if gateway == nil {
		return nil, fmt.Errorf("LLM gateway is required")
	}
	return &Service{db: db, contexts: contexts, plans: planStore, gateway: gateway, recorder: recorder}, nil
}

func (s *Service) Start(ctx context.Context, planID, provider, model string) (Run, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return Run{}, plans.ErrNotFound
	}
	provider, model = defaultProviderAndModel(provider, model)

	planningContext, err := s.contexts.Current(ctx, planID)
	if err != nil {
		return Run{}, err
	}
	plan, err := s.plans.Get(ctx, planID)
	if err != nil {
		return Run{}, err
	}
	resolved, err := s.resolvedAnswers(ctx, planningContext)
	if err != nil {
		return Run{}, err
	}
	active, err := s.activeRun(ctx, planID)
	if err != nil {
		return Run{}, err
	}
	if active.Status == RunStatusNeedsInput {
		if inputs, ok := answerInputsForRun(active, resolved); ok {
			return s.Answer(ctx, active.ID, inputs)
		}
	}

	run, err := s.createRun(ctx, planningContext, "", provider, model)
	if err != nil {
		return Run{}, err
	}
	if _, err := s.plans.Update(ctx, plan.ID, plan.Title, plans.StatusPlanning); err != nil {
		s.failRun(ctx, run, err)
		return Run{}, err
	}
	s.record(ctx, "planning.agent_started", map[string]any{
		"run_id": run.ID, "plan_id": run.PlanID, "plan_version_id": run.PlanVersionID,
		"planning_context_id": run.PlanningContextID, "provider": provider, "model": model,
	}, run.CreatedAt)
	return s.execute(ctx, run, planningContext, resolved, plan)
}

func (s *Service) Answer(ctx context.Context, runID string, inputs []AnswerInput) (Run, error) {
	run, err := s.Get(ctx, strings.TrimSpace(runID))
	if err != nil {
		return Run{}, err
	}
	if run.Status != RunStatusNeedsInput {
		return Run{}, ErrRunNotAwaitingInput
	}
	answerMap := make(map[string]string, len(inputs))
	for _, input := range inputs {
		questionID := strings.TrimSpace(input.QuestionID)
		answer := strings.TrimSpace(input.Answer)
		if questionID != "" && answer != "" {
			answerMap[questionID] = answer
		}
	}
	resolved := make([]ResolvedQuestion, 0, len(run.Questions))
	for _, question := range run.Questions {
		answer := answerMap[question.ID]
		if question.Status != QuestionStatusOpen || answer == "" {
			return Run{}, fmt.Errorf("%w: every open question requires an answer", ErrInvalidAnswers)
		}
		resolved = append(resolved, ResolvedQuestion{Question: question.Question, Answer: answer})
	}

	planningContext, err := s.contexts.Current(ctx, run.PlanID)
	if err != nil {
		return Run{}, err
	}
	if planningContext.ID != run.PlanningContextID || planningContext.PlanVersionID != run.PlanVersionID {
		return Run{}, ErrStaleRun
	}
	plan, err := s.plans.Get(ctx, run.PlanID)
	if err != nil {
		return Run{}, err
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, fmt.Errorf("begin planning answer transaction: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE planning_runs SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?
	`, RunStatusSuperseded, now.UnixMilli(), run.ID, RunStatusNeedsInput)
	if err != nil {
		return Run{}, fmt.Errorf("supersede planning run: %w", err)
	}
	if err := requireOne(result, ErrRunNotAwaitingInput); err != nil {
		return Run{}, err
	}
	for _, question := range run.Questions {
		answer := answerMap[question.ID]
		result, err := tx.ExecContext(ctx, `
			UPDATE planning_questions
			SET answer = ?, status = ?, answered_at = ?
			WHERE id = ? AND run_id = ? AND status = ?
		`, answer, QuestionStatusAnswered, now.UnixMilli(), question.ID, run.ID, QuestionStatusOpen)
		if err != nil {
			return Run{}, fmt.Errorf("answer planning question: %w", err)
		}
		if err := requireOne(result, ErrInvalidAnswers); err != nil {
			return Run{}, err
		}
	}
	child := newRun(planningContext, run.ID, run.Provider, run.Model, now)
	if err := insertRun(ctx, tx, child); err != nil {
		return Run{}, err
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("commit planning answers: %w", err)
	}

	if _, err := s.plans.Update(ctx, plan.ID, plan.Title, plans.StatusPlanning); err != nil {
		s.failRun(ctx, child, err)
		return Run{}, err
	}
	s.record(ctx, "planning.answers_submitted", map[string]any{
		"run_id": run.ID, "next_run_id": child.ID, "plan_id": run.PlanID,
		"answer_count": len(resolved),
	}, now)
	return s.execute(ctx, child, planningContext, resolved, plan)
}

func (s *Service) Current(ctx context.Context, planID string) (Run, error) {
	var runID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM planning_runs WHERE plan_id = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1
	`, strings.TrimSpace(planID)).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrRunNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("read current planning run: %w", err)
	}
	return s.Get(ctx, runID)
}

func (s *Service) resolvedAnswers(ctx context.Context, planningContext planningcontext.Context) ([]ResolvedQuestion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT q.question, q.answer
		FROM planning_questions q
		JOIN planning_runs r ON r.id = q.run_id
		WHERE r.plan_id = ?
			AND r.plan_version_id = ?
			AND r.planning_context_id = ?
			AND q.status = ?
			AND TRIM(COALESCE(q.answer, '')) <> ''
		ORDER BY r.created_at DESC, q.answered_at DESC, q.position ASC, q.id DESC
	`, planningContext.PlanID, planningContext.PlanVersionID, planningContext.ID, QuestionStatusAnswered)
	if err != nil {
		return nil, fmt.Errorf("read resolved planning answers: %w", err)
	}
	defer rows.Close()

	answers := make([]ResolvedQuestion, 0)
	seenQuestions := make(map[string]struct{})
	for rows.Next() {
		var question, answer string
		if err := rows.Scan(&question, &answer); err != nil {
			return nil, fmt.Errorf("scan resolved planning answer: %w", err)
		}
		question = strings.TrimSpace(question)
		answer = strings.TrimSpace(answer)
		if question == "" || answer == "" {
			continue
		}
		if _, exists := seenQuestions[question]; exists {
			continue
		}
		seenQuestions[question] = struct{}{}
		answers = append(answers, ResolvedQuestion{Question: question, Answer: answer})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resolved planning answers: %w", err)
	}
	return answers, nil
}

func (s *Service) activeRun(ctx context.Context, planID string) (Run, error) {
	run, err := s.Current(ctx, planID)
	if errors.Is(err, ErrRunNotFound) {
		return Run{}, nil
	}
	if err != nil {
		return Run{}, fmt.Errorf("read active planning run: %w", err)
	}
	if run.Status != RunStatusRunning && run.Status != RunStatusNeedsInput {
		return Run{}, nil
	}
	return run, nil
}

func answerInputsForRun(run Run, resolved []ResolvedQuestion) ([]AnswerInput, bool) {
	answersByQuestion := make(map[string]string, len(resolved))
	for _, answer := range resolved {
		question := strings.TrimSpace(answer.Question)
		value := strings.TrimSpace(answer.Answer)
		if question != "" && value != "" {
			answersByQuestion[question] = value
		}
	}

	inputs := make([]AnswerInput, 0, len(run.Questions))
	for _, question := range run.Questions {
		if question.Status != QuestionStatusOpen {
			continue
		}
		answer, ok := answersByQuestion[strings.TrimSpace(question.Question)]
		if !ok {
			return nil, false
		}
		inputs = append(inputs, AnswerInput{QuestionID: question.ID, Answer: answer})
	}
	return inputs, len(inputs) > 0
}

func (s *Service) Get(ctx context.Context, runID string) (Run, error) {
	var run Run
	var responseJSON, usageJSON string
	var parentRunID, errorCode, errorMessage sql.NullString
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, plan_id, plan_version_id, planning_context_id, parent_run_id,
			provider, model, status, COALESCE(response_json, ''), usage_json,
			error_code, error_message, created_at, updated_at
		FROM planning_runs WHERE id = ?
	`, strings.TrimSpace(runID)).Scan(
		&run.ID, &run.PlanID, &run.PlanVersionID, &run.PlanningContextID, &parentRunID,
		&run.Provider, &run.Model, &run.Status, &responseJSON, &usageJSON,
		&errorCode, &errorMessage, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrRunNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("read planning run: %w", err)
	}
	run.ParentRunID = parentRunID.String
	run.ErrorCode = llm.ErrorCode(errorCode.String)
	run.ErrorMessage = errorMessage.String
	run.CreatedAt = time.UnixMilli(createdAt).UTC()
	run.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	if responseJSON != "" {
		var result Plan
		if err := json.Unmarshal([]byte(responseJSON), &result); err != nil {
			return Run{}, fmt.Errorf("decode planning run result: %w", err)
		}
		run.Result = &result
	}
	if strings.TrimSpace(usageJSON) != "" {
		if err := json.Unmarshal([]byte(usageJSON), &run.Usage); err != nil {
			return Run{}, fmt.Errorf("decode planning run usage: %w", err)
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, position, question, COALESCE(answer, ''), status, created_at, answered_at
		FROM planning_questions WHERE run_id = ? ORDER BY position ASC
	`, run.ID)
	if err != nil {
		return Run{}, fmt.Errorf("list planning questions: %w", err)
	}
	defer rows.Close()
	run.Questions = make([]Question, 0)
	for rows.Next() {
		var question Question
		var questionCreatedAt int64
		var answeredAt sql.NullInt64
		if err := rows.Scan(
			&question.ID, &question.RunID, &question.Position, &question.Question,
			&question.Answer, &question.Status, &questionCreatedAt, &answeredAt,
		); err != nil {
			return Run{}, fmt.Errorf("scan planning question: %w", err)
		}
		question.CreatedAt = time.UnixMilli(questionCreatedAt).UTC()
		if answeredAt.Valid {
			value := time.UnixMilli(answeredAt.Int64).UTC()
			question.AnsweredAt = &value
		}
		run.Questions = append(run.Questions, question)
	}
	if err := rows.Err(); err != nil {
		return Run{}, fmt.Errorf("iterate planning questions: %w", err)
	}
	return run, nil
}

func (s *Service) createRun(
	ctx context.Context,
	planningContext planningcontext.Context,
	parentRunID, provider, model string,
) (Run, error) {
	now := time.Now().UTC()
	run := newRun(planningContext, parentRunID, provider, model, now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, fmt.Errorf("begin planning run: %w", err)
	}
	defer tx.Rollback()
	var activeID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM planning_runs
		WHERE plan_id = ? AND status IN (?, ?) LIMIT 1
	`, run.PlanID, RunStatusRunning, RunStatusNeedsInput).Scan(&activeID)
	if err == nil {
		return Run{}, fmt.Errorf("%w: %s", ErrActiveRun, activeID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("check active planning run: %w", err)
	}
	if err := insertRun(ctx, tx, run); err != nil {
		return Run{}, err
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("commit planning run: %w", err)
	}
	return run, nil
}

func (s *Service) execute(
	ctx context.Context,
	run Run,
	planningContext planningcontext.Context,
	answers []ResolvedQuestion,
	plan plans.Plan,
) (Run, error) {
	request, err := BuildRequestWithAnswers(planningContext, run.Model, answers)
	if err != nil {
		s.failRun(ctx, run, err)
		return Run{}, err
	}
	response, err := s.gateway.Complete(ctx, run.Provider, request)
	if err != nil {
		s.failRun(ctx, run, err)
		return Run{}, err
	}
	result, err := ParseResponse(response)
	if err != nil {
		s.failRun(ctx, run, err)
		return Run{}, err
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.failRun(ctx, run, err)
		return Run{}, fmt.Errorf("marshal planning result: %w", err)
	}
	usageJSON, err := json.Marshal(response.Usage)
	if err != nil {
		s.failRun(ctx, run, err)
		return Run{}, fmt.Errorf("marshal planning usage: %w", err)
	}

	status := RunStatusCompleted
	planStatus := plans.StatusReady
	if len(result.OpenQuestions) > 0 {
		status = RunStatusNeedsInput
		planStatus = plans.StatusNeedsInput
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.failRun(ctx, run, err)
		return Run{}, fmt.Errorf("begin planning result transaction: %w", err)
	}
	defer tx.Rollback()
	updateResult, err := tx.ExecContext(ctx, `
		UPDATE planning_runs
		SET status = ?, response_json = ?, usage_json = ?, updated_at = ?
		WHERE id = ? AND status = ?
	`, status, string(resultJSON), string(usageJSON), now.UnixMilli(), run.ID, RunStatusRunning)
	if err != nil {
		return Run{}, fmt.Errorf("store planning result: %w", err)
	}
	if err := requireOne(updateResult, ErrRunNotFound); err != nil {
		return Run{}, err
	}
	for position, question := range result.OpenQuestions {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO planning_questions (
				id, run_id, position, question, status, created_at
			) VALUES (?, ?, ?, ?, ?, ?)
		`, newPlanningID(), run.ID, position, question, QuestionStatusOpen, now.UnixMilli()); err != nil {
			return Run{}, fmt.Errorf("store planning question: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("commit planning result: %w", err)
	}
	if _, err := s.plans.Update(ctx, plan.ID, plan.Title, planStatus); err != nil {
		return Run{}, err
	}

	eventType := "planning.agent_completed"
	if status == RunStatusNeedsInput {
		eventType = "planning.agent_needs_input"
	}
	s.record(ctx, eventType, map[string]any{
		"run_id": run.ID, "plan_id": run.PlanID, "status": status,
		"step_count": len(result.Steps), "question_count": len(result.OpenQuestions),
		"input_tokens": response.Usage.InputTokens, "output_tokens": response.Usage.OutputTokens,
	}, now)
	return s.Get(ctx, run.ID)
}

func (s *Service) RecoverInterruptedRuns(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, plan_id FROM planning_runs WHERE status = ?
	`, RunStatusRunning)
	if err != nil {
		return 0, fmt.Errorf("list interrupted planning runs: %w", err)
	}
	interrupted := make([]Run, 0)
	for rows.Next() {
		var run Run
		if err := rows.Scan(&run.ID, &run.PlanID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan interrupted planning run: %w", err)
		}
		interrupted = append(interrupted, run)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate interrupted planning runs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close interrupted planning runs: %w", err)
	}
	if len(interrupted) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	message := "planning was interrupted before completion; start it again"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin interrupted planning recovery: %w", err)
	}
	defer tx.Rollback()
	for _, run := range interrupted {
		result, err := tx.ExecContext(ctx, `
			UPDATE planning_runs
			SET status = ?, error_code = ?, error_message = ?, updated_at = ?
			WHERE id = ? AND status = ?
		`, RunStatusCancelled, llm.ErrorCancelled, message, now.UnixMilli(), run.ID, RunStatusRunning)
		if err != nil {
			return 0, fmt.Errorf("recover interrupted planning run: %w", err)
		}
		if err := requireOne(result, ErrRunNotFound); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE plans SET status = ?, updated_at = ?
			WHERE id = ? AND status = ?
		`, plans.StatusDraft, now.UnixMilli(), run.PlanID, plans.StatusPlanning); err != nil {
			return 0, fmt.Errorf("reset interrupted plan: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit interrupted planning recovery: %w", err)
	}
	for _, run := range interrupted {
		s.record(ctx, "planning.agent_interrupted", map[string]any{
			"run_id": run.ID, "plan_id": run.PlanID, "status": RunStatusCancelled,
		}, now)
	}
	return len(interrupted), nil
}

func (s *Service) failRun(ctx context.Context, run Run, cause error) {
	status := RunStatusFailed
	code := llm.ErrorUnknown
	message := "planning agent failed"
	if providerError, ok := llm.AsProviderError(cause); ok {
		code = providerError.Code
		message = providerError.Message
		if providerError.Code == llm.ErrorCancelled {
			status = RunStatusCancelled
		}
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(cleanupCtx, `
		UPDATE planning_runs
		SET status = ?, error_code = ?, error_message = ?, updated_at = ?
		WHERE id = ? AND status = ?
	`, status, code, message, now.UnixMilli(), run.ID, RunStatusRunning)
	if err != nil {
		slog.Warn("store failed planning run", "run_id", run.ID, "error", err)
	}
	if plan, err := s.plans.Get(cleanupCtx, run.PlanID); err == nil {
		if _, err := s.plans.Update(cleanupCtx, plan.ID, plan.Title, plans.StatusDraft); err != nil {
			slog.Warn("reset failed plan status", "plan_id", run.PlanID, "error", err)
		}
	}
	s.record(cleanupCtx, "planning.agent_failed", map[string]any{
		"run_id": run.ID, "plan_id": run.PlanID, "status": status, "error_code": code,
	}, now)
}

func newRun(
	planningContext planningcontext.Context,
	parentRunID, provider, model string,
	now time.Time,
) Run {
	return Run{
		ID:                newPlanningID(),
		PlanID:            planningContext.PlanID,
		PlanVersionID:     planningContext.PlanVersionID,
		PlanningContextID: planningContext.ID,
		ParentRunID:       strings.TrimSpace(parentRunID),
		Provider:          provider,
		Model:             model,
		Status:            RunStatusRunning,
		Questions:         []Question{},
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func insertRun(ctx context.Context, tx *sql.Tx, run Run) error {
	var parent any
	if run.ParentRunID != "" {
		parent = run.ParentRunID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO planning_runs (
			id, plan_id, plan_version_id, planning_context_id, parent_run_id,
			provider, model, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.PlanID, run.PlanVersionID, run.PlanningContextID, parent,
		run.Provider, run.Model, run.Status, run.CreatedAt.UnixMilli(), run.UpdatedAt.UnixMilli()); err != nil {
		return fmt.Errorf("insert planning run: %w", err)
	}
	return nil
}

func defaultProviderAndModel(provider, model string) (string, string) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" {
		provider = LocalFakeProviderName
	}
	if model == "" {
		model = LocalFakeModelName
	}
	return provider, model
}

func requireOne(result sql.Result, fallback error) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected row count: %w", err)
	}
	if count != 1 {
		return fallback
	}
	return nil
}

func (s *Service) record(ctx context.Context, eventType string, payload any, createdAt time.Time) {
	if s.recorder == nil {
		return
	}
	if err := s.recorder.Record(ctx, "", eventType, payload, createdAt); err != nil {
		slog.Warn("record planning agent activity", "event_type", eventType, "error", err)
	}
}

func newPlanningID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate planning ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}
