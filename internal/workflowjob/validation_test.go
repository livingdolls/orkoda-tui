package workflowjob

import (
	"errors"
	"testing"
)

func TestValidateBaseBranchAcceptsCommonGitBranchNames(t *testing.T) {
	valid := []string{
		"main",
		"feature/workflow-jobs",
		"release/2026.08",
		"fix_issue-123",
	}
	for _, branch := range valid {
		if err := validateBaseBranch(branch); err != nil {
			t.Fatalf("validateBaseBranch(%q) error = %v", branch, err)
		}
	}
}

func TestValidateBaseBranchRejectsUnsafeNames(t *testing.T) {
	invalid := []string{
		"",
		"-leading-option",
		"feature..name",
		"refs@{1}",
		"feature name",
		"feature?name",
		"feature/",
		"feature.",
		"feature\nname",
	}
	for _, branch := range invalid {
		if err := validateBaseBranch(branch); !errors.Is(err, ErrInvalidJob) {
			t.Fatalf("validateBaseBranch(%q) error = %v", branch, err)
		}
	}
}

func TestExecutorContinuationCodes(t *testing.T) {
	if !isExecutorPauseCode("EXECUTOR_BUDGET_EXHAUSTED") || isExecutorPauseCode("EXECUTOR_FAILED") {
		t.Fatal("unexpected executor pause code classification")
	}
	if next, err := nextStatus(StatusFailed, ActionContinueExecution, StatusExecuting); err != nil || next != StatusQueued {
		t.Fatalf("continue next = %s, %v", next, err)
	}
}
