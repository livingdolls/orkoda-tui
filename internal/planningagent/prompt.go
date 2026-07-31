package planningagent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
)

var ResponseSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["summary", "steps", "open_questions", "risks"],
  "properties": {
    "summary": {"type": "string", "minLength": 1},
    "steps": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "title", "description", "affected_files", "acceptance_criteria"],
        "properties": {
          "id": {"type": "string", "minLength": 1},
          "title": {"type": "string", "minLength": 1},
          "description": {"type": "string", "minLength": 1},
          "affected_files": {"type": "array", "items": {"type": "string"}},
          "acceptance_criteria": {"type": "array", "items": {"type": "string"}}
        }
      }
    },
    "open_questions": {"type": "array", "items": {"type": "string"}},
    "risks": {"type": "array", "items": {"type": "string"}}
  }
}`)

func BuildRequest(planningContext planningcontext.Context, model string) (llm.Request, error) {
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

	return llm.Request{
		Model: model,
		Messages: []llm.Message{
			{
				Role: llm.RoleSystem,
				Content: strings.TrimSpace(`You are Orkoda's software planning agent.
Create an implementation plan grounded only in the supplied normalized context.
Return one JSON object matching the response schema exactly.
Do not include Markdown fences, prose outside JSON, or invented repository files.
Use open_questions when essential information is missing instead of guessing.`),
			},
			{
				Role: llm.RoleUser,
				Content: "Create a safe, testable implementation plan for this context:\n" + string(contextJSON),
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
		},
	}, nil
}
