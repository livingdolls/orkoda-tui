package planningagent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
)

var ResponseSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["summary", "steps", "open_questions", "risks"],
  "properties": {
    "summary": {"type": "string", "minLength": 1, "maxLength": 4000},
    "steps": {
      "type": "array",
      "maxItems": 100,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "title", "description", "affected_files", "acceptance_criteria"],
        "properties": {
          "id": {"type": "string", "minLength": 1, "maxLength": 128},
          "title": {"type": "string", "minLength": 1, "maxLength": 500},
          "description": {"type": "string", "minLength": 1, "maxLength": 8000},
          "affected_files": {
            "type": "array",
            "maxItems": 200,
            "items": {"type": "string", "minLength": 1, "maxLength": 1024}
          },
          "acceptance_criteria": {
            "type": "array",
            "minItems": 1,
            "maxItems": 100,
            "items": {"type": "string", "minLength": 1, "maxLength": 2000}
          }
        }
      }
    },
    "open_questions": {
      "type": "array",
      "maxItems": 50,
      "items": {"type": "string", "minLength": 1, "maxLength": 2000}
    },
    "risks": {
      "type": "array",
      "maxItems": 100,
      "items": {"type": "string", "minLength": 1, "maxLength": 2000}
    }
  }
}`)

type ResolvedQuestion struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

func BuildRequest(planningContext planningcontext.Context, model string) (llm.Request, error) {
	return BuildRequestWithAnswers(planningContext, model, nil)
}

func BuildRequestWithAnswers(
	planningContext planningcontext.Context,
	model string,
	answers []ResolvedQuestion,
) (llm.Request, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return llm.Request{}, fmt.Errorf("planning model is required")
	}
	if strings.TrimSpace(planningContext.ID) == "" || strings.TrimSpace(planningContext.PlanID) == "" {
		return llm.Request{}, fmt.Errorf("planning context and plan IDs are required")
	}
	if planningContext.PlanVersion <= 0 {
		return llm.Request{}, fmt.Errorf("planning context version must be positive")
	}

	contextJSON, err := json.MarshalIndent(planningContext.NormalizedPlan, "", "  ")
	if err != nil {
		return llm.Request{}, fmt.Errorf("marshal normalized planning context: %w", err)
	}

	userContent := "Create a safe, testable implementation plan for this context:\n" + string(contextJSON)
	normalizedAnswers := make([]ResolvedQuestion, 0, len(answers))
	for _, answer := range answers {
		answer.Question = strings.TrimSpace(answer.Question)
		answer.Answer = strings.TrimSpace(answer.Answer)
		if answer.Question == "" || answer.Answer == "" {
			continue
		}
		normalizedAnswers = append(normalizedAnswers, answer)
	}
	if len(normalizedAnswers) > 0 {
		answersJSON, err := json.MarshalIndent(normalizedAnswers, "", "  ")
		if err != nil {
			return llm.Request{}, fmt.Errorf("marshal resolved planning questions: %w", err)
		}
		userContent += "\n\nResolved questions supplied by the user:\n" + string(answersJSON)
	}

	return llm.Request{
		Model: model,
		Messages: []llm.Message{
			{
				Role: llm.RoleSystem,
				Content: strings.TrimSpace(`You are Orkoda's software planning agent.
Create an implementation plan grounded only in the supplied normalized context.
Return one JSON object matching the response schema exactly.
Do not include Markdown fences, prose outside JSON, or invented repository files.
Use open_questions when essential information is missing instead of guessing.
Treat resolved questions as authoritative user input.`),
			},
			{
				Role:    llm.RoleUser,
				Content: userContent,
			},
		},
		ResponseSchema:  append(json.RawMessage(nil), ResponseSchema...),
		MaxOutputTokens: 4096,
		Temperature:     0.1,
		Metadata: map[string]string{
			"plan_id":               planningContext.PlanID,
			"plan_version":          fmt.Sprintf("%d", planningContext.PlanVersion),
			"planning_context_id":   planningContext.ID,
			"repository_summary_id": planningContext.RepositorySummaryID,
			"answered_questions":    strconv.Itoa(len(normalizedAnswers)),
		},
	}, nil
}
