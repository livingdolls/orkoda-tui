package reviewer

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/checks"
	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/planningagent"
)

func TestContextBuilderBoundsEvidenceAndBuildsStableCriteria(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "context.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	criteriaJSON, _ := json.Marshal([]string{"Returns the expected value."})
	constraintsJSON, _ := json.Marshal([]string{"Do not add network access."})
	if _, err := db.ExecContext(ctx, `
		INSERT INTO plan_versions (
			id, plan_id, version, requirement, acceptance_criteria_json,
			constraints_json, created_at
		) VALUES ('plan-version-1','plan-1',1,'Implement the feature.',?,?,1)
	`, string(criteriaJSON), string(constraintsJSON)); err != nil {
		t.Fatal(err)
	}
	planJSON, _ := json.Marshal(planningagent.Plan{
		Summary: "Implementation plan",
		Steps: []planningagent.Step{
			{ID: "step-1", Title: "Implement", Description: "Implement it", AcceptanceCriteria: []string{"Tests pass."}},
		},
		OpenQuestions: []string{},
		Risks:         []string{},
	})
	if _, err := db.ExecContext(ctx, `
		INSERT INTO planning_runs (
			id, plan_id, plan_version_id, planning_context_id, provider, model,
			status, response_json, usage_json, created_at, updated_at
		) VALUES ('planning-1','plan-1','plan-version-1','context-1','local-fake','model','COMPLETED',?,'{}',1,1)
	`, string(planJSON)); err != nil {
		t.Fatal(err)
	}
	builder, err := NewContextBuilder(db)
	if err != nil {
		t.Fatal(err)
	}
	builder.maxPatchBytes = 8
	builder.maxCheckOutputBytes = 5
	changedFiles, _ := json.Marshal([]string{"internal/example.go", "internal/example.go"})
	reviewContext, validation, err := builder.Build(
		ctx,
		"plan-version-1",
		execution.Execution{ExecutionVersion: 1, BaseCommitSHA: "abc123"},
		execution.Checkpoint{
			PatchChecksum:    "sha256:patch",
			PatchText:        "1234567890",
			ChangedFilesJSON: changedFiles,
		},
		checks.Run{Status: checks.StatusFailed},
		[]checks.Step{{Profile: "go.test", Status: checks.StatusFailed, OutputText: "abcdefghij"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if reviewContext.Patch != "12345678" || !reviewContext.PatchTruncated {
		t.Fatalf("bounded patch = %q truncated=%v", reviewContext.Patch, reviewContext.PatchTruncated)
	}
	if len(reviewContext.ChangedFiles) != 1 || len(reviewContext.Checks[0].Output) != 5 {
		t.Fatalf("context = %#v", reviewContext)
	}
	for _, criterion := range []string{"requirement.ac-1", "plan.step-1.ac-1"} {
		if _, exists := validation.CriteriaRefs[criterion]; !exists {
			t.Fatalf("criterion %q missing from validation context", criterion)
		}
	}
	if _, exists := validation.ChangedFiles["internal/example.go"]; !exists {
		t.Fatal("changed file missing from validation context")
	}
}

func TestTruncateBytesPreservesUTF8(t *testing.T) {
	value, truncated := truncateBytes(strings.Repeat("é", 8), 5)
	if !truncated || !json.Valid([]byte(`"`+value+`"`)) {
		t.Fatalf("truncateBytes() value=%q truncated=%v", value, truncated)
	}
}
