package planningcontext

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/repositorysummary"
)

var (
	ErrNotFound       = errors.New("planning context not found")
	ErrPlanNotFound   = errors.New("plan not found")
	ErrSummaryMissing = errors.New("current repository summary is required")
)

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type SummaryReader interface {
	Current(context.Context, string) (repositorysummary.Summary, error)
}

type RepositoryContext struct {
	RepositoryID    string                     `json:"repository_id"`
	SummaryID       string                     `json:"summary_id"`
	HeadSHA         string                     `json:"head_sha"`
	Dirty           bool                       `json:"dirty"`
	Languages       []string                   `json:"languages"`
	Frameworks      []string                   `json:"frameworks"`
	PackageManagers []string                   `json:"package_managers"`
	Commands        repositorysummary.Commands `json:"commands"`
	ImportantFiles  []string                   `json:"important_files"`
}

type NormalizedPlan struct {
	Goal               string            `json:"goal"`
	Summary            string            `json:"summary"`
	Scope              []string          `json:"scope"`
	NonGoals           []string          `json:"non_goals"`
	AcceptanceCriteria []string          `json:"acceptance_criteria"`
	Constraints        []string          `json:"constraints"`
	AffectedAreas      []string          `json:"affected_areas"`
	Risks              []string          `json:"risks"`
	OpenQuestions      []string          `json:"open_questions"`
	Repository         RepositoryContext `json:"repository"`
}

type Context struct {
	ID                  string         `json:"id"`
	PlanID              string         `json:"plan_id"`
	PlanVersionID       string         `json:"plan_version_id"`
	PlanVersion         int            `json:"plan_version"`
	RepositorySummaryID string         `json:"repository_summary_id"`
	NormalizedPlan      NormalizedPlan `json:"normalized_plan"`
	CreatedAt           time.Time      `json:"created_at"`
}

type planState struct {
	PlanID             string
	ProjectID          string
	Title              string
	PlanVersionID      string
	PlanVersion        int
	Requirement        string
	AcceptanceCriteria []string
	Constraints        []string
	RepositoryID       string
}

type Repository struct {
	db        *sql.DB
	summaries SummaryReader
	recorder  EventRecorder
}

func NewRepository(db *sql.DB, summaries SummaryReader, recorder EventRecorder) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if summaries == nil {
		return nil, fmt.Errorf("repository summary reader is required")
	}
	return &Repository{db: db, summaries: summaries, recorder: recorder}, nil
}

func (r *Repository) Normalize(ctx context.Context, planID string) (Context, error) {
	state, err := r.planState(ctx, strings.TrimSpace(planID))
	if err != nil {
		return Context{}, err
	}

	startedAt := time.Now().UTC()
	r.record(ctx, "plan.normalization_started", map[string]any{
		"plan_id":       state.PlanID,
		"project_id":    state.ProjectID,
		"plan_version":  state.PlanVersion,
		"repository_id": state.RepositoryID,
	}, startedAt)

	summary, err := r.summaries.Current(ctx, state.RepositoryID)
	if err != nil {
		if errors.Is(err, repositorysummary.ErrNotFound) {
			err = ErrSummaryMissing
		}
		r.recordFailure(ctx, state, err)
		return Context{}, err
	}
	if existing, err := r.get(ctx, state, summary); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		r.recordFailure(ctx, state, err)
		return Context{}, err
	}

	normalized := normalize(state, summary)
	normalizedJSON, err := json.Marshal(normalized)
	if err != nil {
		r.recordFailure(ctx, state, err)
		return Context{}, fmt.Errorf("marshal normalized plan: %w", err)
	}
	createdAt := time.Now().UTC()
	if _, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO planning_contexts (
			id, plan_id, plan_version_id, repository_summary_id, normalized_plan_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, newID(), state.PlanID, state.PlanVersionID, summary.ID, string(normalizedJSON), createdAt.UnixMilli()); err != nil {
		r.recordFailure(ctx, state, err)
		return Context{}, fmt.Errorf("store planning context: %w", err)
	}

	planningContext, err := r.get(ctx, state, summary)
	if err != nil {
		r.recordFailure(ctx, state, err)
		return Context{}, err
	}
	r.record(ctx, "plan.normalized", map[string]any{
		"plan_id":               state.PlanID,
		"project_id":            state.ProjectID,
		"plan_version":          state.PlanVersion,
		"repository_id":         state.RepositoryID,
		"repository_summary_id": summary.ID,
		"head_sha":              summary.HeadSHA,
		"planning_context_id":   planningContext.ID,
	}, planningContext.CreatedAt)
	return planningContext, nil
}

func (r *Repository) Current(ctx context.Context, planID string) (Context, error) {
	state, err := r.planState(ctx, strings.TrimSpace(planID))
	if err != nil {
		return Context{}, err
	}
	summary, err := r.summaries.Current(ctx, state.RepositoryID)
	if err != nil {
		if errors.Is(err, repositorysummary.ErrNotFound) {
			return Context{}, ErrSummaryMissing
		}
		return Context{}, err
	}
	return r.get(ctx, state, summary)
}

func (r *Repository) planState(ctx context.Context, planID string) (planState, error) {
	if planID == "" {
		return planState{}, ErrPlanNotFound
	}

	var state planState
	var criteriaJSON, constraintsJSON string
	err := r.db.QueryRowContext(ctx, `
		SELECT
			p.id, p.project_id, p.title,
			pv.id, pv.version, pv.requirement,
			pv.acceptance_criteria_json, pv.constraints_json,
			r.id
		FROM plans p
		JOIN plan_versions pv
			ON pv.plan_id = p.id AND pv.version = p.current_version
		JOIN repositories r ON r.project_id = p.project_id
		WHERE p.id = ?
		ORDER BY r.created_at ASC
		LIMIT 1
	`, planID).Scan(
		&state.PlanID,
		&state.ProjectID,
		&state.Title,
		&state.PlanVersionID,
		&state.PlanVersion,
		&state.Requirement,
		&criteriaJSON,
		&constraintsJSON,
		&state.RepositoryID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return planState{}, ErrPlanNotFound
	}
	if err != nil {
		return planState{}, fmt.Errorf("read plan normalization state: %w", err)
	}
	if err := json.Unmarshal([]byte(criteriaJSON), &state.AcceptanceCriteria); err != nil {
		return planState{}, fmt.Errorf("decode acceptance criteria: %w", err)
	}
	if err := json.Unmarshal([]byte(constraintsJSON), &state.Constraints); err != nil {
		return planState{}, fmt.Errorf("decode constraints: %w", err)
	}
	return state, nil
}

func (r *Repository) get(ctx context.Context, state planState, summary repositorysummary.Summary) (Context, error) {
	var planningContext Context
	var normalizedJSON string
	var createdAt int64
	err := r.db.QueryRowContext(ctx, `
		SELECT id, plan_id, plan_version_id, repository_summary_id, normalized_plan_json, created_at
		FROM planning_contexts
		WHERE plan_version_id = ? AND repository_summary_id = ?
	`, state.PlanVersionID, summary.ID).Scan(
		&planningContext.ID,
		&planningContext.PlanID,
		&planningContext.PlanVersionID,
		&planningContext.RepositorySummaryID,
		&normalizedJSON,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Context{}, ErrNotFound
	}
	if err != nil {
		return Context{}, fmt.Errorf("read planning context: %w", err)
	}
	if err := json.Unmarshal([]byte(normalizedJSON), &planningContext.NormalizedPlan); err != nil {
		return Context{}, fmt.Errorf("decode planning context: %w", err)
	}
	planningContext.PlanVersion = state.PlanVersion
	planningContext.CreatedAt = time.UnixMilli(createdAt).UTC()
	return planningContext, nil
}

func normalize(state planState, summary repositorysummary.Summary) NormalizedPlan {
	scope := append([]string(nil), state.AcceptanceCriteria...)
	if len(scope) == 0 {
		scope = requirementScope(state.Requirement)
	}

	affectedAreas := stringSet{}
	for _, language := range summary.Snapshot.Languages {
		switch language {
		case "TypeScript", "JavaScript":
			affectedAreas.Add("frontend")
		case "Go", "PHP", "Python", "Ruby", "Rust", "Java":
			affectedAreas.Add("backend")
		case "Kotlin", "Swift", "Dart":
			affectedAreas.Add("mobile")
		case "SQL":
			affectedAreas.Add("database")
		}
	}
	for _, framework := range summary.Snapshot.Frameworks {
		switch framework {
		case "Next.js", "React", "OpenTUI", "Vue", "Svelte", "Vite":
			affectedAreas.Add("frontend")
		case "Android", "Jetpack Compose":
			affectedAreas.Add("mobile")
		}
	}

	risks := make([]string, 0)
	if summary.Dirty {
		risks = append(risks, "Repository has uncommitted changes that may affect the planning baseline.")
	}
	if summary.Snapshot.Truncated {
		risks = append(risks, "Repository scan reached its file limit, so some areas may be missing from context.")
	}
	if len(summary.Snapshot.Commands) == 0 {
		risks = append(risks, "No standard test, lint, format, or build commands were detected.")
	}

	openQuestions := make([]string, 0)
	if len(state.AcceptanceCriteria) == 0 {
		openQuestions = append(openQuestions, "Which observable outcomes define completion for this plan?")
	}
	if len(state.Constraints) == 0 {
		openQuestions = append(openQuestions, "Are there compatibility, dependency, or deployment constraints?")
	}
	if summary.Dirty {
		openQuestions = append(openQuestions, "Should uncommitted repository changes be included in the implementation baseline?")
	}

	return NormalizedPlan{
		Goal:               state.Title,
		Summary:            strings.TrimSpace(state.Requirement),
		Scope:              scope,
		NonGoals:           []string{},
		AcceptanceCriteria: append([]string(nil), state.AcceptanceCriteria...),
		Constraints:        append([]string(nil), state.Constraints...),
		AffectedAreas:      affectedAreas.Sorted(),
		Risks:              risks,
		OpenQuestions:      openQuestions,
		Repository: RepositoryContext{
			RepositoryID:    summary.RepositoryID,
			SummaryID:       summary.ID,
			HeadSHA:         summary.HeadSHA,
			Dirty:           summary.Dirty,
			Languages:       append([]string(nil), summary.Snapshot.Languages...),
			Frameworks:      append([]string(nil), summary.Snapshot.Frameworks...),
			PackageManagers: append([]string(nil), summary.Snapshot.PackageManagers...),
			Commands:        cloneCommands(summary.Snapshot.Commands),
			ImportantFiles:  append([]string(nil), summary.Snapshot.ImportantFiles...),
		},
	}
}

func requirementScope(requirement string) []string {
	replacer := strings.NewReplacer("\r", "", "\n", ". ", ";", ".")
	parts := strings.Split(replacer.Replace(requirement), ".")
	result := make([]string, 0, 8)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
		if len(result) == 8 {
			break
		}
	}
	if len(result) == 0 && strings.TrimSpace(requirement) != "" {
		result = append(result, strings.TrimSpace(requirement))
	}
	return result
}

func cloneCommands(commands repositorysummary.Commands) repositorysummary.Commands {
	result := repositorysummary.Commands{}
	for category, values := range commands {
		result[category] = append([]string(nil), values...)
	}
	return result
}

func (r *Repository) recordFailure(ctx context.Context, state planState, cause error) {
	r.record(ctx, "plan.normalization_failed", map[string]any{
		"plan_id":       state.PlanID,
		"project_id":    state.ProjectID,
		"plan_version":  state.PlanVersion,
		"repository_id": state.RepositoryID,
		"error":         cause.Error(),
	}, time.Now().UTC())
}

func (r *Repository) record(ctx context.Context, eventType string, payload any, createdAt time.Time) {
	if r.recorder == nil {
		return
	}
	if err := r.recorder.Record(ctx, "", eventType, payload, createdAt); err != nil {
		slog.Warn("record planning context activity", "event_type", eventType, "error", err)
	}
}

type stringSet map[string]struct{}

func (s stringSet) Add(value string) {
	if value = strings.TrimSpace(value); value != "" {
		s[value] = struct{}{}
	}
}

func (s stringSet) Sorted() []string {
	values := make([]string, 0, len(s))
	for value := range s {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate planning context ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}
