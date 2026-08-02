package planningagent

import (
	"strings"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/llm"
)

func TestParseResponseRejectsUnsafeAffectedFiles(t *testing.T) {
	for _, unsafePath := range []string{"../secret.txt", "/etc/passwd", "folder/../../secret"} {
		t.Run(strings.ReplaceAll(unsafePath, "/", "_"), func(t *testing.T) {
			response := llm.Response{Content: `{
				"summary":"unsafe",
				"steps":[{
					"id":"step-1",
					"title":"Unsafe path",
					"description":"Must be rejected",
					"affected_files":["` + unsafePath + `"],
					"acceptance_criteria":["Path is safe"]
				}],
				"open_questions":[],
				"risks":[]
			}`}
			if _, err := ParseResponse(response); err == nil {
				t.Fatalf("expected unsafe path %q to be rejected", unsafePath)
			}
		})
	}
}

func TestParseResponseRequiresAcceptanceCriteria(t *testing.T) {
	response := llm.Response{Content: `{
		"summary":"missing criteria",
		"steps":[{
			"id":"step-1",
			"title":"Implement",
			"description":"Implement feature",
			"affected_files":["internal/feature.go"],
			"acceptance_criteria":[]
		}],
		"open_questions":[],
		"risks":[]
	}`}
	if _, err := ParseResponse(response); err == nil {
		t.Fatal("expected missing acceptance criteria to be rejected")
	}
}

func TestParseResponseNormalizesRepositoryRelativePaths(t *testing.T) {
	response := llm.Response{Content: `{
		"summary":"safe",
		"steps":[{
			"id":"step-1",
			"title":"Implement",
			"description":"Implement feature",
			"affected_files":["internal\\feature.go","docs/./guide.md"],
			"acceptance_criteria":["Checks pass"]
		}],
		"open_questions":[],
		"risks":[]
	}`}
	plan, err := ParseResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(plan.Steps[0].AffectedFiles, ","); got != "internal/feature.go,docs/guide.md" {
		t.Fatalf("unexpected normalized paths: %s", got)
	}
}
