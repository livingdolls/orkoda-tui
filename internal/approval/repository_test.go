package approval

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestRepositoryPersistsVersionBoundDecisionAndRevision(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "approval.db"))
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
		WorkflowJobID:         "workflow-1",
		ReviewRunID:           "review-1",
		ExecutionID:           "execution-1",
		ExecutionVersion:      1,
		CheckpointID:          "checkpoint-1",
		BaseCommitSHA:         "abc123",
		PatchChecksum:         "sha256:123",
		Kind:                  KindRequestRevision,
		Note:                  "Add the missing regression test.",
		RevisionInstructions:  "Add the missing regression test.",
		ReviewerVerdict:       "REQUEST_REVISION",
		WorkflowVersionBefore: 8,
		RevisionCountBefore:   0,
	}
	decision, created, err := repository.CreateOrGet(ctx, input)
	if err != nil || !created {
		t.Fatalf("CreateOrGet() decision=%#v created=%v error=%v", decision, created, err)
	}
	if decision.RevisionInstructions != input.RevisionInstructions || decision.Status != StatusPending {
		t.Fatalf("decision = %#v", decision)
	}
	duplicate, created, err := repository.CreateOrGet(ctx, input)
	if err != nil || created || duplicate.ID != decision.ID {
		t.Fatalf("duplicate=%#v created=%v error=%v", duplicate, created, err)
	}
	conflict := input
	conflict.PatchChecksum = "sha256:different"
	if _, _, err := repository.CreateOrGet(ctx, conflict); err != ErrSnapshotConflict {
		t.Fatalf("conflict error = %v", err)
	}
	applied, err := repository.MarkApplied(ctx, decision.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Status != StatusApplied || applied.WorkflowVersionAfter != 10 || applied.AppliedAt == nil {
		t.Fatalf("applied = %#v", applied)
	}
	items, err := repository.ListWorkflow(ctx, "workflow-1")
	if err != nil || len(items) != 1 || items[0].RevisionInstructions == "" {
		t.Fatalf("items=%#v error=%v", items, err)
	}
}

func TestRepositoryRejectsInvalidDecisionText(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repository, _ := NewRepository(db)
	_, _, err = repository.CreateOrGet(ctx, CreateInput{
		WorkflowJobID: "workflow-1", ReviewRunID: "review-1", ExecutionID: "execution-1",
		ExecutionVersion: 1, CheckpointID: "checkpoint-1", BaseCommitSHA: "abc",
		PatchChecksum: "sha256:1", Kind: KindRequestRevision, ReviewerVerdict: "APPROVE",
		WorkflowVersionBefore: 1,
	})
	if err == nil {
		t.Fatal("expected missing revision instructions to fail")
	}
}
