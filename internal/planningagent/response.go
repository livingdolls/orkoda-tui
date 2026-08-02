package planningagent

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
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
	if len(plan.Steps) > 100 {
		return fmt.Errorf("planning response cannot contain more than 100 steps")
	}
	if len(plan.Steps) == 0 && len(nonEmpty(plan.OpenQuestions)) == 0 {
		return fmt.Errorf("planning response requires steps or open questions")
	}
	if err := validateNonEmptyList("open question", plan.OpenQuestions); err != nil {
		return err
	}
	if err := validateNonEmptyList("risk", plan.Risks); err != nil {
		return err
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
		if len(step.AcceptanceCriteria) == 0 {
			return fmt.Errorf("planning step %q requires at least one acceptance criterion", stepID)
		}
		if err := validateNonEmptyList(fmt.Sprintf("planning step %q acceptance criterion", stepID), step.AcceptanceCriteria); err != nil {
			return err
		}
		for fileIndex, fileName := range step.AffectedFiles {
			if err := validateAffectedFile(fileName); err != nil {
				return fmt.Errorf("planning step %q affected file %d: %w", stepID, fileIndex, err)
			}
		}
	}
	return nil
}

func validateNonEmptyList(label string, values []string) error {
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s %d cannot be empty", label, index)
		}
	}
	return nil
}

func validateAffectedFile(value string) error {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return fmt.Errorf("path is required")
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("path contains a null byte")
	}
	if strings.HasPrefix(value, "/") {
		return fmt.Errorf("path must be repository-relative")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("path escapes the repository root")
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
		plan.Steps[index].AffectedFiles = normalizePaths(plan.Steps[index].AffectedFiles)
		plan.Steps[index].AcceptanceCriteria = nonEmpty(plan.Steps[index].AcceptanceCriteria)
	}
	return plan
}

func normalizePaths(values []string) []string {
	result := nonEmpty(values)
	for index := range result {
		result[index] = path.Clean(strings.ReplaceAll(result[index], "\\", "/"))
	}
	return result
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
