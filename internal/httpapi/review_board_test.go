package httpapi

import (
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/reviewer"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

func TestResolveReviewBoardColumn(t *testing.T) {
	tests := []struct {
		name   string
		job    workflowjob.Job
		review *reviewer.Run
		want   string
	}{
		{name: "first review queued", job: workflowjob.Job{Status: workflowjob.StatusReviewing, ExecutionVersion: 1}, want: "AWAITING_REVIEW"},
		{name: "review running", job: workflowjob.Job{Status: workflowjob.StatusReviewing, ExecutionVersion: 1}, review: &reviewer.Run{Status: reviewer.StatusRunning}, want: "AI_REVIEWING"},
		{name: "blocking findings", job: workflowjob.Job{Status: workflowjob.StatusWaitingForApproval, ExecutionVersion: 1}, review: &reviewer.Run{Verdict: reviewer.VerdictRequestRevision, BlockingIssues: 1}, want: "ISSUES_FOUND"},
		{name: "revision execution", job: workflowjob.Job{Status: workflowjob.StatusExecuting, ExecutionVersion: 2, RevisionCount: 1}, want: "REVISION_IN_PROGRESS"},
		{name: "re-review", job: workflowjob.Job{Status: workflowjob.StatusReviewing, ExecutionVersion: 2, RevisionCount: 1}, want: "RE_REVIEW"},
		{name: "ready for approval", job: workflowjob.Job{Status: workflowjob.StatusWaitingForApproval, ExecutionVersion: 1}, review: &reviewer.Run{Verdict: reviewer.VerdictApprove}, want: "READY_FOR_APPROVAL"},
		{name: "approved", job: workflowjob.Job{Status: workflowjob.StatusCompleted, ExecutionVersion: 1}, want: "APPROVED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveReviewBoardColumn(test.job, test.review); got != test.want {
				t.Fatalf("resolveReviewBoardColumn() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestReviewBoardRelevance(t *testing.T) {
	if isReviewBoardRelevant(workflowjob.Job{Status: workflowjob.StatusReady}) {
		t.Fatal("ready workflow should not appear before execution starts")
	}
	if !isReviewBoardRelevant(workflowjob.Job{Status: workflowjob.StatusChecking, ExecutionVersion: 1}) {
		t.Fatal("checking workflow should appear")
	}
}
