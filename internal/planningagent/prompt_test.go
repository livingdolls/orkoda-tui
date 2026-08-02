package planningagent

import (
	"strings"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
	"github.com/livingdolls/orkoda-tui/internal/repositorysummary"
)

func planningContextFixture() planningcontext.Context {
	return planningcontext.Context{
		ID:                    "context-1",
		PlanID:                "plan-1",
		PlanVersionID:         "version-2",
		PlanVersion:           2,
		RepositoryID:          "repository-1",
		RepositorySummaryID:   "summary-1",
		RepositorySummaryHead: "abc123",
		NormalizedPlan: planningcontext.NormalizedPlan{
			Goal:               "Add Markdown blog",
			Summary:            "Build a local Markdown-backed blog.",
			Scope:              []string{"Load articles", "Render article pages"},
			NonGoals:           []string{"Add a CMS"},
			AcceptanceCriteria: []string{"Articles are listed", "Article pages render"},
			Constraints:        []string{"Keep existing routes stable"},
			AffectedAreas:      []string{"content", "router"},
			Risks:              []string{"Existing slug conflicts"},
			OpenQuestions:      []string{"Where should Markdown live?"},
			Repository: planningcontext.RepositoryMetadata{
				RepositoryID: "repository-1",
				HeadSHA:      "abc123",
				Languages:    []string{"TypeScript"},
				Frameworks:   []string{"React"},
				ImportantFiles: []repositorysummary.ImportantFile{
					{Path: "package.json", Kind: "manifest"},
				},
				TestCommands:  []string{"bun test"},
				LintCommands:  []string{"bun run lint"},
				BuildCommands: []string{"bun run build"},
			},
		},
	}
}

func TestBuildRequestIncludesNormalizedContext(t *testing.T) {
	request, err := BuildRequest(planningContextFixture(), "fake-planner-v1")
	if err != nil {
		t.Fatal(err)
	}
	if request.Model != "fake-planner-v1" || request.MaxOutputTokens == 0 {
		t.Fatalf("unexpected request: %#v", request)
	}
	if len(request.Messages) != 2 || request.Messages[0].Role != llm.RoleSystem {
		t.Fatalf("unexpected messages: %#v", request.Messages)
	}
	if !strings.Contains(request.Messages[0].Content, "Return one JSON object") {
		t.Fatalf("system prompt does not request structured output: %s", request.Messages[0].Content)
	}
	userMessage := request.Messages[1].Content
	for _, expected := range []string{
		"Add Markdown blog",
		"Load articles",
		"Articles are listed",
		"Keep existing routes stable",
		"TypeScript",
		"React",
		"package.json",
		"bun test",
		"Existing slug conflicts",
		"Where should Markdown live?",
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
			"affected_files":[" internal/blog/loader.go "],
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
		providerError, ok := llm.AsProviderError(err)
		if !ok || providerError.Code != llm.ErrorInvalidResponse {
			t.Fatalf("unexpected error for %s: %v", content, err)
		}
	}
}
