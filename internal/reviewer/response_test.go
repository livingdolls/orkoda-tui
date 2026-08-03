package reviewer

import (
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/llm"
)

func TestParseResponseAcceptsValidatedApproval(t *testing.T) {
	response := llm.Response{Content: `{
		"verdict":"APPROVE",
		"summary":"Implementation matches the supplied criteria.",
		"issues":[{
			"key":"minor-naming",
			"severity":"LOW",
			"category":"MAINTAINABILITY",
			"blocking":false,
			"title":"Minor naming improvement",
			"description":"A future cleanup could improve the helper name.",
			"file_path":"internal/example.go",
			"line_start":12,
			"line_end":12,
			"criteria_refs":["requirement.ac-1"]
		}]
	}`}
	result, err := ParseResponse(response, ValidationContext{
		ChangedFiles: map[string]struct{}{"internal/example.go": {}},
		CriteriaRefs: map[string]struct{}{"requirement.ac-1": {}},
	})
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if result.Verdict != VerdictApprove || len(result.Issues) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseResponseRejectsUnsafeOrInconsistentIssues(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "approve with blocking issue",
			content: reviewJSON("APPROVE", `{
				"key":"blocking","severity":"HIGH","category":"CORRECTNESS","blocking":true,
				"title":"Bug","description":"Must be fixed.","file_path":"internal/example.go",
				"line_start":1,"line_end":1,"criteria_refs":[]
			}`),
		},
		{
			name: "revision without blocking issue",
			content: reviewJSON("REQUEST_REVISION", `{
				"key":"advice","severity":"LOW","category":"MAINTAINABILITY","blocking":false,
				"title":"Advice","description":"Optional.","file_path":"","line_start":0,
				"line_end":0,"criteria_refs":[]
			}`),
		},
		{
			name: "unknown changed file",
			content: reviewJSON("REQUEST_REVISION", `{
				"key":"unknown-file","severity":"HIGH","category":"CORRECTNESS","blocking":true,
				"title":"Bug","description":"Wrong file.","file_path":"internal/other.go",
				"line_start":1,"line_end":1,"criteria_refs":[]
			}`),
		},
		{
			name: "path traversal",
			content: reviewJSON("REQUEST_REVISION", `{
				"key":"escape","severity":"HIGH","category":"SECURITY","blocking":true,
				"title":"Escape","description":"Invalid path.","file_path":"../secret",
				"line_start":1,"line_end":1,"criteria_refs":[]
			}`),
		},
		{
			name: "unknown criterion",
			content: reviewJSON("REQUEST_REVISION", `{
				"key":"criterion","severity":"HIGH","category":"REQUIREMENT","blocking":true,
				"title":"Missing","description":"Criterion mismatch.","file_path":"",
				"line_start":0,"line_end":0,"criteria_refs":["unknown"]
			}`),
		},
		{
			name: "critical non blocking",
			content: reviewJSON("APPROVE", `{
				"key":"critical","severity":"CRITICAL","category":"SECURITY","blocking":false,
				"title":"Critical","description":"Must block.","file_path":"","line_start":0,
				"line_end":0,"criteria_refs":[]
			}`),
		},
	}
	validation := ValidationContext{
		ChangedFiles: map[string]struct{}{"internal/example.go": {}},
		CriteriaRefs: map[string]struct{}{"requirement.ac-1": {}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseResponse(llm.Response{Content: test.content}, validation); err == nil {
				t.Fatal("ParseResponse() expected an error")
			}
		})
	}
}

func reviewJSON(verdict string, issue string) string {
	return `{"verdict":"` + verdict + `","summary":"review summary","issues":[` + issue + `]}`
}
