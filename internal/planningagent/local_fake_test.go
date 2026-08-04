package planningagent

import (
	"context"
	"strings"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
)

func TestLocalFakeProviderQuestionsThenPlan(t *testing.T) {
	planningContext := planningcontext.Context{
		ID:                  "context-1",
		PlanID:              "plan-1",
		PlanVersionID:       "version-1",
		PlanVersion:         1,
		RepositorySummaryID: "summary-1",
		NormalizedPlan: planningcontext.NormalizedPlan{
			Goal:               "Add Markdown blog",
			Summary:            "Build a Markdown blog.",
			Scope:              []string{"Load Markdown articles"},
			AcceptanceCriteria: []string{"Articles can be listed"},
			OpenQuestions:      []string{"Which directory stores Markdown files?"},
			Repository: planningcontext.RepositoryContext{
				RepositoryID:   "repository-1",
				SummaryID:      "summary-1",
				HeadSHA:        "abc123",
				ImportantFiles: []string{"go.mod", "cmd/api/main.go"},
			},
		},
	}
	provider := NewLocalFakeProvider()

	request, err := BuildRequest(planningContext, LocalFakeModelName)
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Complete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertLocalFakePlanningResponse(t, response)
	questionPlan, err := ParseResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(questionPlan.OpenQuestions) != 1 || len(questionPlan.Steps) != 0 {
		t.Fatalf("expected an open-question response, got %#v", questionPlan)
	}

	request, err = BuildRequestWithAnswers(planningContext, LocalFakeModelName, []ResolvedQuestion{{
		Question: "Which directory stores Markdown files?",
		Answer:   "content/blog",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(request.Messages[1].Content, "content/blog") {
		t.Fatal("resolved answer was not added to the planning prompt")
	}
	response, err = provider.Complete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertLocalFakePlanningResponse(t, response)
	completedPlan, err := ParseResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(completedPlan.OpenQuestions) != 0 || len(completedPlan.Steps) != 1 {
		t.Fatalf("expected a completed deterministic plan, got %#v", completedPlan)
	}
	if completedPlan.Steps[0].AffectedFiles[0] != "go.mod" {
		t.Fatalf("expected repository files in the generated plan, got %#v", completedPlan.Steps[0])
	}
}

func assertLocalFakePlanningResponse(t *testing.T, response llm.Response) {
	t.Helper()
	_, issues := (llm.JSONSchemaValidator{}).Validate(ResponseSchema, response.Content)
	if len(issues) > 0 {
		t.Fatalf("local fake response failed the planning schema: %#v; content=%s", issues, response.Content)
	}
}
