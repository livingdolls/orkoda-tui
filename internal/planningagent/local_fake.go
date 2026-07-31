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

type LocalFakeProvider struct{}

func NewLocalFakeProvider() *LocalFakeProvider {
	return &LocalFakeProvider{}
}

func (p *LocalFakeProvider) Name() string {
	return LocalFakeProviderName
}

func (p *LocalFakeProvider) Complete(ctx context.Context, request llm.Request) (llm.Response, error) {
	select {
	case <-ctx.Done():
		return llm.Response{}, ctx.Err()
	default:
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
	}, nil
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
