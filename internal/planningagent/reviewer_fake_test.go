package planningagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/llm"
)

func TestLocalFakeReviewerUsesPersistedCheckOutcome(t *testing.T) {
	provider := NewLocalFakeProvider()
	response, err := provider.Complete(context.Background(), llm.Request{
		Model: LocalFakeModelName,
		Metadata: map[string]string{
			"agent_role":    "reviewer",
			"failed_checks": "1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Verdict string `json:"verdict"`
		Issues  []struct {
			Blocking bool `json:"blocking"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(response.Content), &result); err != nil {
		t.Fatal(err)
	}
	if result.Verdict != "REQUEST_REVISION" || len(result.Issues) != 1 || !result.Issues[0].Blocking {
		t.Fatalf("result = %#v", result)
	}
}
