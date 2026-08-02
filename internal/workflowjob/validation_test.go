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
