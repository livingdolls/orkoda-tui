package reviewer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/livingdolls/orkoda-tui/internal/checks"
	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/planningagent"
)

const (
	defaultMaxPatchBytes       = 128 * 1024
	defaultMaxCheckOutputBytes = 32 * 1024
	maxCheckStepOutputBytes    = 4 * 1024
)

type Criterion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type CheckEvidence struct {
	Profile         string        `json:"profile"`
	Status          checks.Status `json:"status"`
	ExitCode        *int          `json:"exit_code,omitempty"`
	DurationMS      int64         `json:"duration_ms"`
	Output          string        `json:"output,omitempty"`
	OutputTruncated bool          `json:"output_truncated"`
	ErrorMessage    string        `json:"error_message,omitempty"`
}

type Context struct {
	Requirement        string             `json:"requirement"`
	AcceptanceCriteria []Criterion        `json:"acceptance_criteria"`
	Constraints        []string           `json:"constraints"`
	ImplementationPlan planningagent.Plan `json:"implementation_plan"`
	ExecutionVersion   int                `json:"execution_version"`
	BaseCommitSHA      string             `json:"base_commit_sha"`
	PatchChecksum      string             `json:"patch_checksum"`
	ChangedFiles       []string           `json:"changed_files"`
	Patch              string             `json:"patch"`
	PatchTruncated     bool               `json:"patch_truncated"`
	CheckStatus        checks.Status      `json:"check_status"`
	Checks             []CheckEvidence    `json:"checks"`
}

type ValidationContext struct {
	ChangedFiles map[string]struct{}
	CriteriaRefs map[string]struct{}
}

type ContextBuilder struct {
	db                  *sql.DB
	maxPatchBytes       int
	maxCheckOutputBytes int
}

func NewContextBuilder(db *sql.DB) (*ContextBuilder, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	return &ContextBuilder{
		db:                  db,
		maxPatchBytes:       defaultMaxPatchBytes,
		maxCheckOutputBytes: defaultMaxCheckOutputBytes,
	}, nil
}

func (b *ContextBuilder) Build(
	ctx context.Context,
	planVersionID string,
	executionItem execution.Execution,
	checkpoint execution.Checkpoint,
	checkRun checks.Run,
	checkSteps []checks.Step,
) (Context, ValidationContext, error) {
	var requirement, criteriaJSON, constraintsJSON string
	err := b.db.QueryRowContext(ctx, `
		SELECT requirement, acceptance_criteria_json, constraints_json
		FROM plan_versions WHERE id = ?
	`, strings.TrimSpace(planVersionID)).Scan(&requirement, &criteriaJSON, &constraintsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return Context{}, ValidationContext{}, fmt.Errorf("plan version is not available")
	}
	if err != nil {
		return Context{}, ValidationContext{}, fmt.Errorf("load reviewer plan version: %w", err)
	}
	var acceptanceCriteria, constraints []string
	if err := json.Unmarshal([]byte(criteriaJSON), &acceptanceCriteria); err != nil {
		return Context{}, ValidationContext{}, fmt.Errorf("decode acceptance criteria: %w", err)
	}
	if err := json.Unmarshal([]byte(constraintsJSON), &constraints); err != nil {
		return Context{}, ValidationContext{}, fmt.Errorf("decode plan constraints: %w", err)
	}

	implementationPlan, err := b.loadImplementationPlan(ctx, planVersionID)
	if err != nil {
		return Context{}, ValidationContext{}, err
	}
	changedFiles, err := decodeChangedFiles(checkpoint.ChangedFilesJSON)
	if err != nil {
		return Context{}, ValidationContext{}, err
	}
	patch, patchTruncated := truncateBytes(checkpoint.PatchText, b.maxPatchBytes)
	criteria := buildCriteria(acceptanceCriteria, implementationPlan)
	checksEvidence := buildCheckEvidence(checkSteps, b.maxCheckOutputBytes)

	validation := ValidationContext{
		ChangedFiles: make(map[string]struct{}, len(changedFiles)),
		CriteriaRefs: make(map[string]struct{}, len(criteria)),
	}
	for _, fileName := range changedFiles {
		validation.ChangedFiles[fileName] = struct{}{}
	}
	for _, criterion := range criteria {
		validation.CriteriaRefs[criterion.ID] = struct{}{}
	}

	return Context{
		Requirement:        strings.TrimSpace(requirement),
		AcceptanceCriteria: criteria,
		Constraints:        normalizedStrings(constraints),
		ImplementationPlan: implementationPlan,
		ExecutionVersion:   executionItem.ExecutionVersion,
		BaseCommitSHA:      executionItem.BaseCommitSHA,
		PatchChecksum:      checkpoint.PatchChecksum,
		ChangedFiles:       changedFiles,
		Patch:              patch,
		PatchTruncated:     patchTruncated,
		CheckStatus:        checkRun.Status,
		Checks:             checksEvidence,
	}, validation, nil
}

func (b *ContextBuilder) loadImplementationPlan(ctx context.Context, planVersionID string) (planningagent.Plan, error) {
	var responseJSON string
	err := b.db.QueryRowContext(ctx, `
		SELECT response_json FROM planning_runs
		WHERE plan_version_id = ? AND status = 'COMPLETED' AND response_json IS NOT NULL
		ORDER BY created_at DESC, rowid DESC LIMIT 1
	`, strings.TrimSpace(planVersionID)).Scan(&responseJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return planningagent.Plan{
			Summary:       "No generated implementation plan was persisted for this plan version.",
			Steps:         []planningagent.Step{},
			OpenQuestions: []string{},
			Risks:         []string{},
		}, nil
	}
	if err != nil {
		return planningagent.Plan{}, fmt.Errorf("load implementation plan: %w", err)
	}
	var result planningagent.Plan
	if err := json.Unmarshal([]byte(responseJSON), &result); err != nil {
		return planningagent.Plan{}, fmt.Errorf("decode implementation plan: %w", err)
	}
	return result, nil
}

func decodeChangedFiles(raw json.RawMessage) ([]string, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode checkpoint changed files: %w", err)
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = path.Clean(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
		if value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "/") {
			return nil, fmt.Errorf("checkpoint contains invalid changed file %q", value)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func buildCriteria(base []string, plan planningagent.Plan) []Criterion {
	result := make([]Criterion, 0, len(base)+len(plan.Steps)*2)
	for index, value := range normalizedStrings(base) {
		result = append(result, Criterion{ID: fmt.Sprintf("requirement.ac-%d", index+1), Text: value})
	}
	for _, step := range plan.Steps {
		stepID := strings.TrimSpace(step.ID)
		if stepID == "" {
			continue
		}
		for index, value := range normalizedStrings(step.AcceptanceCriteria) {
			result = append(result, Criterion{
				ID:   fmt.Sprintf("plan.%s.ac-%d", stepID, index+1),
				Text: value,
			})
		}
	}
	return result
}

func buildCheckEvidence(steps []checks.Step, totalLimit int) []CheckEvidence {
	remaining := totalLimit
	result := make([]CheckEvidence, 0, len(steps))
	for _, step := range steps {
		outputLimit := maxCheckStepOutputBytes
		if remaining < outputLimit {
			outputLimit = remaining
		}
		output, truncated := truncateBytes(step.OutputText, outputLimit)
		remaining -= len([]byte(output))
		result = append(result, CheckEvidence{
			Profile:         step.Profile,
			Status:          step.Status,
			ExitCode:        step.ExitCode,
			DurationMS:      step.DurationMS,
			Output:          output,
			OutputTruncated: step.OutputTruncated || truncated,
			ErrorMessage:    truncateString(strings.TrimSpace(step.ErrorMessage), 1024),
		})
	}
	return result
}

func normalizedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func truncateBytes(value string, limit int) (string, bool) {
	if limit <= 0 {
		return "", value != ""
	}
	payload := []byte(value)
	if len(payload) <= limit {
		return value, false
	}
	payload = payload[:limit]
	for len(payload) > 0 && !utf8.Valid(payload) {
		payload = payload[:len(payload)-1]
	}
	return string(payload), true
}

func truncateString(value string, limit int) string {
	result, _ := truncateBytes(value, limit)
	return result
}
