package reviewer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/llm"
)

func TestRepositoryPersistsReviewAndIssuesAtomically(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "review.db"))
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
	repository, err := NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	input := CreateInput{
		WorkflowJobID:        "workflow-1",
		ExecutionID:          "execution-1",
		ExecutionVersion:     1,
		CheckRunID:           "check-1",
		CheckpointID:         "checkpoint-1",
		AgentSettingsVersion: 2,
		Provider:             "local-fake",
		Model:                "reviewer-model",
	}
	run, created, err := repository.CreateOrGet(ctx, input)
	if err != nil || !created {
		t.Fatalf("CreateOrGet() run=%#v created=%v error=%v", run, created, err)
	}
	duplicate, created, err := repository.CreateOrGet(ctx, input)
	if err != nil || created || duplicate.ID != run.ID {
		t.Fatalf("duplicate run=%#v created=%v error=%v", duplicate, created, err)
	}
	conflict := input
	conflict.CheckpointID = "checkpoint-2"
	if _, _, err := repository.CreateOrGet(ctx, conflict); err != ErrSnapshotConflict {
		t.Fatalf("snapshot conflict error = %v", err)
	}
	if _, err := repository.Start(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := repository.Complete(ctx, run.ID, Result{
		Verdict: VerdictRequestRevision,
		Summary: "One blocking issue was found.",
		Issues: []Issue{
			{
				Key:          "issue-1",
				Severity:     SeverityHigh,
				Category:     CategoryCorrectness,
				Blocking:     true,
				Title:        "Incorrect result",
				Description:  "The changed implementation returns the wrong value.",
				FilePath:     "internal/example.go",
				LineStart:    10,
				LineEnd:      12,
				CriteriaRefs: []string{"requirement.ac-1"},
			},
		},
	}, llm.Usage{InputTokens: 100, OutputTokens: 40, TotalTokens: 140})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusCompleted || completed.BlockingIssues != 1 || completed.TotalIssues != 1 {
		t.Fatalf("completed = %#v", completed)
	}
	issues, err := repository.ListIssues(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Key != "issue-1" || issues[0].CriteriaRefs[0] != "requirement.ac-1" {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestRepositoryCanRestartFailedReview(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "review-retry.db"))
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
	repository, _ := NewRepository(db)
	run, _, err := repository.CreateOrGet(ctx, CreateInput{
		WorkflowJobID:        "workflow-1",
		ExecutionID:          "execution-1",
		ExecutionVersion:     1,
		CheckRunID:           "check-1",
		CheckpointID:         "checkpoint-1",
		AgentSettingsVersion: 1,
		Provider:             "local-fake",
		Model:                "reviewer-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Fail(ctx, run.ID, "UNAVAILABLE", "temporary failure", false); err != nil {
		t.Fatal(err)
	}
	restarted, err := repository.Start(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Status != StatusRunning || restarted.FailureCode != "" || restarted.FailureMessage != "" {
		t.Fatalf("restarted = %#v", restarted)
	}
}
