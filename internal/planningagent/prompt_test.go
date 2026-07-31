package planningagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
	"github.com/livingdolls/orkoda-tui/internal/repositorysummary"
)

func planningContextFixture() planningcontext.Context {
	return planningcontext.Context{
		ID:                  "context-1",
		PlanID:              "plan-1",
		PlanVersionID:       "version-1",
		PlanVersion:         2,
		RepositorySummaryID: "summary-1",
		NormalizedPlan: planningcontext.NormalizedPlan{
			Goal:               "Add Markdown blog",
			Summary:            "Add article listing, search, and detail pages.",
			Scope:              []string{"List articles", "Search articles"},
			NonGoals:           []string{"External CMS"},
			AcceptanceCriteria: []string{"Users can search articles"},
			Constraints:        []string{"Use the existing stack"},
			AffectedAreas:      []string{"frontend", "backend"},
			Risks:              []string{"Repository has uncommitted changes"},
			OpenQuestions:      []string{"Where should Markdown files live?"},
			Repository: planningcontext.RepositoryContext{
				RepositoryID:    "repository-1",
				SummaryID:       "summary-1",
				HeadSHA:         "abc123",
				Dirty:           true,
				Languages:       []string{"Go", "TypeScript"},
				Frameworks:      []string{"Gin", "OpenTUI"},
				PackageManagers: []string{"Go Modules", "Bun"},
				Commands: repositorysummary.Commands{
					"test":  []string{"go test ./...", "bun test"},
					"build": []string{"go build ./cmd/api"},
				},
				ImportantFiles: []string{"go.mod", "package.json", "cmd/api/main.go"},
			},
		},
	}
}

func TestBuildRequestUsesNormalizedPlanningContext(t *testing.T) {
	request, err := BuildRequest(planningContextFixture(), "fake-planner-v1")
	if err != nil {
		t.Fatal(err)
	}
	if request.Model != "fake-planner-v1" || len(request.Messages) != 2 {
		t.Fatalf("unexpected request: %#v", request)
	}
	if !json.Valid(request.ResponseSchema) {
		t.Fatal("response schema is not valid JSON")
	}
	userMessage := request.Messages[1].Content
	for _, expected := range []string{
		"Add Markdown blog",
		"Go",
		"TypeScript",
		"abc123",
		"go test ./...",
		"Where should Markdown files live?",
	} {
		if !strings.Contains(userMessage, expected) {
			t.Fatalf("planning prompt is missing %q: %s", expected, userMessage)
		}
	}
	if request.Metadata["plan_id"] != "plan-1" || request.Metadata["plan_version"] != "2" {
		t.Fatalf("unexpected request metadata: %#v", request.Metadata)
	}
}

func TestBuildRequestRejectsIncompleteContext(t *testing.T) {
	planningContext := planningContextFixture()
	planningContext.ID = ""
	if _, err := BuildRequest(planningContext, "fake"); err == nil {
		t.Fatal("expected missing context ID error")
	}
	if _, err := BuildRequest(planningContextFixture(), " "); err == nil {
		t.Fatal("expected missing model error")
	}
}

func TestParseResponseValidatesAndNormalizesPlan(t *testing.T) {
	response := llm.Response{Content: `{
		"summary":"  Implement the blog  ",
		"steps":[{
			"id":" step-1 ",
			"title":" Add loader ",
			"description":" Read Markdown files ",
			"affected_files":[" internal/blog/loader.go ", ""],
			"acceptance_criteria":[" Articles load "]
		}],
		"open_questions":[],
		"risks":[" Migration compatibility "]
	}`}
	plan, err := ParseResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary != "Implement the blog" || plan.Steps[0].ID != "step-1" {
		t.Fatalf("response was not normalized: %#v", plan)
	}
	if len(plan.Steps[0].AffectedFiles) != 1 || plan.Risks[0] != "Migration compatibility" {
		t.Fatalf("unexpected normalized lists: %#v", plan)
	}
}

func TestParseResponseRejectsInvalidJSONAndSchema(t *testing.T) {
	cases := []string{
		`not json`,
		`{"summary":"Plan","steps":[],"open_questions":[],"risks":[]}`,
		`{"summary":"Plan","steps":[{"id":"same","title":"One","description":"Do one","affected_files":[],"acceptance_criteria":[]},{"id":"same","title":"Two","description":"Do two","affected_files":[],"acceptance_criteria":[]}],"open_questions":[],"risks":[]}`,
		`{"summary":"Plan","steps":[],"open_questions":["Question"],"risks":[],"extra":true}`,
	}
	for _, content := range cases {
		_, err := ParseResponse(llm.Response{Content: content})
		if err == nil {
			t.Fatalf("expected invalid response error for %s", content)
		}
		var providerError *llm.ProviderError
		if !errors.As(err, &providerError) || providerError.Code != llm.ErrorInvalidResponse {
			t.Fatalf("expected INVALID_RESPONSE, got %T: %v", err, err)
		}
	}
}

func TestPlanningRequestRunsThroughFakeGateway(t *testing.T) {
	fake, err := llm.NewFakeProvider("fake", llm.FakeResult{Response: llm.Response{
		ID:           "response-1",
		FinishReason: llm.FinishReasonStop,
		Content: `{
			"summary":"Implement the blog",
			"steps":[{
				"id":"step-1",
				"title":"Add loader",
				"description":"Read Markdown files",
				"affected_files":["internal/blog/loader.go"],
				"acceptance_criteria":["Articles load"]
			}],
			"open_questions":[],
			"risks":[]
		}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := llm.NewRegistry(fake)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llm.NewGateway(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	request, err := BuildRequest(planningContextFixture(), "fake-planner-v1")
	if err != nil {
		t.Fatal(err)
	}
	response, err := gateway.Complete(context.Background(), "fake", request)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ParseResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].AffectedFiles[0] != "internal/blog/loader.go" {
		t.Fatalf("unexpected generated plan: %#v", plan)
	}
	requests := fake.Requests()
	if len(requests) != 1 || requests[0].Metadata["planning_context_id"] != "context-1" {
		t.Fatalf("unexpected fake requests: %#v", requests)
	}
}
