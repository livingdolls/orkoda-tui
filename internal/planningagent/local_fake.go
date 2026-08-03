package planningagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
)

const (
	LocalFakeProviderName = "local-fake"
	LocalFakeModelName    = "local-fake-planner-v1"
)

// LocalFakeProvider keeps the complete planning, executor, and reviewer flow usable without credentials or network access.
type LocalFakeProvider struct{}

func NewLocalFakeProvider() *LocalFakeProvider {
	return &LocalFakeProvider{}
}

func (p *LocalFakeProvider) Name() string {
	return LocalFakeProviderName
}

func (p *LocalFakeProvider) Info() llm.ProviderInfo {
	return llm.ProviderInfo{
		Name:             LocalFakeProviderName,
		DefaultModel:     LocalFakeModelName,
		Configured:       true,
		StructuredOutput: true,
	}
}

func (p *LocalFakeProvider) Complete(ctx context.Context, request llm.Request) (llm.Response, error) {
	select {
	case <-ctx.Done():
		return llm.Response{}, ctx.Err()
	default:
	}

	switch request.Metadata["agent_role"] {
	case "executor":
		return localExecutorResponse(request)
	case "reviewer":
		return localReviewerResponse(request)
	}

	normalized, err := extractNormalizedPlan(request)
	if err != nil {
		return llm.Response{}, &llm.ProviderError{
			Provider: LocalFakeProviderName,
			Code:     llm.ErrorInvalidRequest,
			Message:  "local fake provider could not read the planning context",
			Cause:    err,
		}
	}

	answered, _ := strconv.Atoi(request.Metadata["answered_questions"])
	plan := deterministicPlan(normalized, answered > 0)
	content, err := json.Marshal(plan)
	if err != nil {
		return llm.Response{}, fmt.Errorf("marshal local fake response: %w", err)
	}
	return localResponse(request, content), nil
}

func localExecutorResponse(request llm.Request) (llm.Response, error) {
	content, err := json.Marshal(map[string]any{
		"type":    "finish",
		"summary": "Local fake executor completed the deterministic no-op execution after the workspace foundation was verified.",
	})
	if err != nil {
		return llm.Response{}, fmt.Errorf("marshal local fake executor response: %w", err)
	}
	return localResponse(request, content), nil
}

func localReviewerResponse(request llm.Request) (llm.Response, error) {
	failedChecks, _ := strconv.Atoi(request.Metadata["failed_checks"])
	result := map[string]any{
		"verdict": "APPROVE",
		"summary": "Local fake reviewer found no blocking issue in the deterministic execution snapshot.",
		"issues":  []any{},
	}
	if failedChecks > 0 {
		result["verdict"] = "REQUEST_REVISION"
		result["summary"] = "Local fake reviewer requests revision because one or more persisted repository checks failed."
		result["issues"] = []any{
			map[string]any{
				"key":           "failed-checks",
				"severity":      "HIGH",
				"category":      "TESTING",
				"blocking":      true,
				"title":         "Repository checks failed",
				"description":   "Fix the persisted failing formatter, linter, typecheck, test, or build checks before approval.",
				"file_path":     "",
				"line_start":    0,
				"line_end":      0,
				"criteria_refs": []string{},
			},
		}
	}
	content, err := json.Marshal(result)
	if err != nil {
		return llm.Response{}, fmt.Errorf("marshal local fake reviewer response: %w", err)
	}
	return localResponse(request, content), nil
}

func localResponse(request llm.Request, content []byte) llm.Response {
	inputTokens := 0
	for _, message := range request.Messages {
		inputTokens += max(1, len(message.Content)/4)
	}
	outputTokens := max(1, len(content)/4)
	return llm.Response{
		ID:           "local-fake-response",
		Model:        request.Model,
		Content:      string(content),
		FinishReason: llm.FinishReasonStop,
		Usage: llm.Usage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
		},
	}
}

func extractNormalizedPlan(request llm.Request) (planningcontext.NormalizedPlan, error) {
	for index := len(request.Messages) - 1; index >= 0; index-- {
		message := request.Messages[index]
		if message.Role != llm.RoleUser {
			continue
		}
		marker := "Create a safe, testable implementation plan for this context:\n"
		position := strings.Index(message.Content, marker)
		if position < 0 {
			continue
		}
		payload := message.Content[position+len(marker):]
		if answersPosition := strings.Index(payload, "\n\nResolved questions supplied by the user:\n"); answersPosition >= 0 {
			payload = payload[:answersPosition]
		}
		var normalized planningcontext.NormalizedPlan
		if err := json.Unmarshal([]byte(payload), &normalized); err != nil {
			return planningcontext.NormalizedPlan{}, err
		}
		return normalized, nil
	}
	return planningcontext.NormalizedPlan{}, fmt.Errorf("normalized planning context is missing")
}

func deterministicPlan(normalized planningcontext.NormalizedPlan, hasAnswers bool) Plan {
	if !hasAnswers && len(normalized.OpenQuestions) > 0 {
		return Plan{
			Summary:       "Additional user input is required before implementation planning can be finalized.",
			Steps:         []Step{},
			OpenQuestions: append([]string(nil), normalized.OpenQuestions...),
			Risks:         append([]string(nil), normalized.Risks...),
		}
	}

	scope := append([]string(nil), normalized.Scope...)
	if len(scope) == 0 && strings.TrimSpace(normalized.Goal) != "" {
		scope = []string{normalized.Goal}
	}
	files := normalized.Repository.ImportantFiles
	if len(files) > 3 {
		files = files[:3]
	}
	steps := make([]Step, 0, len(scope))
	for index, item := range scope {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		criteria := append([]string(nil), normalized.AcceptanceCriteria...)
		if len(criteria) == 0 {
			criteria = []string{"The implementation satisfies the planned scope and existing checks pass."}
		}
		steps = append(steps, Step{
			ID:                 fmt.Sprintf("step-%d", index+1),
			Title:              item,
			Description:        "Implement and verify: " + item,
			AffectedFiles:      append([]string(nil), files...),
			AcceptanceCriteria: criteria,
		})
	}
	if len(steps) == 0 {
		steps = append(steps, Step{
			ID:                 "step-1",
			Title:              normalized.Goal,
			Description:        "Implement the normalized plan using the detected repository conventions.",
			AffectedFiles:      append([]string(nil), files...),
			AcceptanceCriteria: []string{"Existing validation commands pass."},
		})
	}
	return Plan{
		Summary:       "Implementation plan for " + normalized.Goal,
		Steps:         steps,
		OpenQuestions: []string{},
		Risks:         append([]string(nil), normalized.Risks...),
	}
}
