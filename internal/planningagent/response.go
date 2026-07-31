package planningagent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/livingdolls/orkoda-tui/internal/llm"
)

type Step struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AffectedFiles      []string `json:"affected_files"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

type Plan struct {
	Summary       string   `json:"summary"`
	Steps         []Step   `json:"steps"`
	OpenQuestions []string `json:"open_questions"`
	Risks         []string `json:"risks"`
}

func ParseResponse(response llm.Response) (Plan, error) {
	decoder := json.NewDecoder(strings.NewReader(response.Content))
	decoder.DisallowUnknownFields()

	var plan Plan
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, invalidResponse(fmt.Sprintf("decode planning response: %v", err), err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Plan{}, invalidResponse("planning response contains trailing content", err)
	}
	if err := validatePlan(plan); err != nil {
		return Plan{}, invalidResponse(err.Error(), err)
	}
	return normalizePlan(plan), nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("unexpected trailing JSON value")
}

func validatePlan(plan Plan) error {
	if strings.TrimSpace(plan.Summary) == "" {
		return fmt.Errorf("planning response summary is required")
	}
	if len(plan.Steps) == 0 && len(nonEmpty(plan.OpenQuestions)) == 0 {
		return fmt.Errorf("planning response requires steps or open questions")
	}
	seen := map[string]struct{}{}
	for index, step := range plan.Steps {
		stepID := strings.TrimSpace(step.ID)
		if stepID == "" {
			return fmt.Errorf("planning step %d ID is required", index)
		}
		if _, exists := seen[stepID]; exists {
			return fmt.Errorf("planning step ID %q is duplicated", stepID)
		}
		seen[stepID] = struct{}{}
		if strings.TrimSpace(step.Title) == "" {
			return fmt.Errorf("planning step %q title is required", stepID)
		}
		if strings.TrimSpace(step.Description) == "" {
			return fmt.Errorf("planning step %q description is required", stepID)
		}
	}
	return nil
}

func normalizePlan(plan Plan) Plan {
	plan.Summary = strings.TrimSpace(plan.Summary)
	plan.OpenQuestions = nonEmpty(plan.OpenQuestions)
	plan.Risks = nonEmpty(plan.Risks)
	for index := range plan.Steps {
		plan.Steps[index].ID = strings.TrimSpace(plan.Steps[index].ID)
		plan.Steps[index].Title = strings.TrimSpace(plan.Steps[index].Title)
		plan.Steps[index].Description = strings.TrimSpace(plan.Steps[index].Description)
		plan.Steps[index].AffectedFiles = nonEmpty(plan.Steps[index].AffectedFiles)
		plan.Steps[index].AcceptanceCriteria = nonEmpty(plan.Steps[index].AcceptanceCriteria)
	}
	return plan
}

func nonEmpty(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func invalidResponse(message string, cause error) error {
	return &llm.ProviderError{
		Code:    llm.ErrorInvalidResponse,
		Message: message,
		Cause:   cause,
	}
}
