package workflowjob

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
)

type recordedEvent struct {
	jobID     string
	eventType string
	payload   any
}

type fakeRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (f *fakeRecorder) Record(_ context.Context, jobID, eventType string, payload any, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, recordedEvent{jobID: jobID, eventType: eventType, payload: payload})
	return nil
}

func openWorkflowRepository(t *testing.T, planStatus string) (*Repository, *jobqueue.Queue, *sql.DB, *fakeRecorder, CreateInput) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatalf("database.Migrate() error = %v", err)
	}

	now := time.Now().UTC().UnixMilli()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, created_at, updated_at)
		VALUES ('project-1', 'Example', ?, ?)
	`, now, now); err != nil {
		db.Close()
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO repositories (
			id, project_id, local_path, current_branch, head_sha,
			remote_url, dirty, created_at, updated_at
		) VALUES ('repository-1', 'project-1', '/tmp/example', 'main',
			'0123456789abcdef', '', 0, ?, ?)
	`, now, now); err != nil {
		db.Close()
		t.Fatalf("insert repository: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO plans (id, project_id, title, status, current_version, created_at, updated_at)
		VALUES ('plan-1', 'project-1', 'Feature', ?, 1, ?, ?)
	`, planStatus, now, now); err != nil {
		db.Close()
		t.Fatalf("insert plan: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO plan_versions (
			id, plan_id, version, requirement, acceptance_criteria_json, constraints_json, created_at
		) VALUES ('plan-version-1', 'plan-1', 1, 'Build it', '[]', '[]', ?)
	`, now); err != nil {
		db.Close()
		t.Fatalf("insert plan version: %v", err)
	}

	queue := jobqueue.New(db)
	recorder := &fakeRecorder{}
	repository, err := NewRepository(db, queue, recorder)
	if err != nil {
		db.Close()
		t.Fatalf("NewRepository() error = %v", err)
	}
	return repository, queue, db, recorder, CreateInput{
		ProjectID:    "project-1",
		PlanID:       "plan-1",
		RepositoryID: "repository-1",
	}
}

func TestCreateAndStartWorkflowAtomicallyEnqueuesDispatch(t *testing.T) {
	repository, queue, db, recorder, input := openWorkflowRepository(t, "READY")
	defer db.Close()
	ctx := context.Background()

	created, err := repository.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != StatusReady || created.Version != 1 || created.BaseBranch != "main" {
		t.Fatalf("created = %#v", created)
	}
	if created.Limits.MaxRevisions != 3 || created.Limits.MaxStageAttempts != 3 ||
		created.Limits.MaxToolCalls != 120 || created.Limits.WallClockSeconds != 3600 {
		t.Fatalf("default limits = %#v", created.Limits)
	}

	started, err := repository.Transition(ctx, created.ID, TransitionInput{
		ExpectedVersion: 1,
		Action:          ActionStart,
		Details:         map[string]any{"requested_by": "local-user"},
	})
	if err != nil {
		t.Fatalf("Transition(START) error = %v", err)
	}
	if started.Status != StatusWorkspacePreparing || started.Version != 2 || started.CurrentDispatchID == "" {
		t.Fatalf("started = %#v", started)
	}

	var jobType, payloadJSON, queueStatus string
	if err := db.QueryRowContext(ctx, `
		SELECT type, payload_json, status FROM jobs WHERE id = ?
	`, started.CurrentDispatchID).Scan(&jobType, &payloadJSON, &queueStatus); err != nil {
		t.Fatalf("read dispatch: %v", err)
	}
	if jobType != "workflow.prepare_workspace" || queueStatus != "QUEUED" {
		t.Fatalf("dispatch type=%q status=%q", jobType, queueStatus)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode dispatch payload: %v", err)
	}
	if payload["workflow_job_id"] != created.ID || payload["workflow_version"] != float64(2) {
		t.Fatalf("dispatch payload = %#v", payload)
	}

	unsupported, err := queue.ClaimTypes(ctx, "worker", time.Now().UTC(), []string{"system.noop"})
	if err != nil {
		t.Fatalf("ClaimTypes(unsupported) error = %v", err)
	}
	if unsupported != nil {
		t.Fatalf("unsupported claim = %#v", unsupported)
	}
	claimed, err := queue.ClaimTypes(ctx, "worker", time.Now().UTC(), []string{"workflow.prepare_workspace"})
	if err != nil {
		t.Fatalf("ClaimTypes(workflow) error = %v", err)
	}
	if claimed == nil || claimed.ID != started.CurrentDispatchID {
		t.Fatalf("claimed = %#v", claimed)
	}

	transitions, err := repository.ListTransitions(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListTransitions() error = %v", err)
	}
	if len(transitions) != 2 || transitions[0].Action != ActionCreate || transitions[1].Action != ActionStart {
		t.Fatalf("transitions = %#v", transitions)
	}
	if len(recorder.events) != 2 || recorder.events[0].eventType != "workflow.created" ||
		recorder.events[1].eventType != "workflow.transitioned" {
		t.Fatalf("events = %#v", recorder.events)
	}
}

func TestWorkflowTransitionTableAndExecutionVersions(t *testing.T) {
	repository, _, db, _, input := openWorkflowRepository(t, "READY")
	defer db.Close()
	ctx := context.Background()

	job, err := repository.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	actions := []Action{
		ActionStart,
		ActionWorkspaceReady,
		ActionExecutionStarted,
		ActionExecutionCompleted,
		ActionChecksCompleted,
		ActionReviewCompleted,
		ActionRequestRevision,
		ActionQueueRevision,
		ActionExecutionStarted,
		ActionExecutionCompleted,
		ActionChecksCompleted,
		ActionReviewCompleted,
		ActionApprove,
		ActionPublish,
		ActionPublicationCompleted,
	}
	for _, action := range actions {
		job, err = repository.Transition(ctx, job.ID, TransitionInput{
			ExpectedVersion: job.Version,
			Action:          action,
		})
		if err != nil {
			t.Fatalf("Transition(%s) error = %v", action, err)
		}
	}
	if job.Status != StatusCompleted || job.ExecutionVersion != 2 || job.RevisionCount != 1 {
		t.Fatalf("completed job = %#v", job)
	}
	if job.CompletedAt == nil {
		t.Fatal("completed_at is nil")
	}
	if _, err := repository.Transition(ctx, job.ID, TransitionInput{
		ExpectedVersion: job.Version,
		Action:          ActionCancel,
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal cancel error = %v", err)
	}
}

func TestFailureRetryAndOptimisticVersioning(t *testing.T) {
	repository, _, db, _, input := openWorkflowRepository(t, "READY")
	defer db.Close()
	ctx := context.Background()

	job, err := repository.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	job, err = repository.Transition(ctx, job.ID, TransitionInput{ExpectedVersion: 1, Action: ActionStart})
	if err != nil {
		t.Fatalf("start error = %v", err)
	}
	if _, err := repository.Transition(ctx, job.ID, TransitionInput{
		ExpectedVersion: 1,
		Action:          ActionFail,
		FailureMessage:  "workspace failed",
	}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale transition error = %v", err)
	}

	failed, err := repository.Transition(ctx, job.ID, TransitionInput{
		ExpectedVersion: job.Version,
		Action:          ActionFail,
		FailureCode:     "WORKTREE_ERROR",
		FailureMessage:  "workspace failed",
	})
	if err != nil {
		t.Fatalf("fail error = %v", err)
	}
	if failed.Status != StatusFailed || failed.RetryStatus != StatusWorkspacePreparing {
		t.Fatalf("failed = %#v", failed)
	}

	retried, err := repository.Transition(ctx, job.ID, TransitionInput{
		ExpectedVersion: failed.Version,
		Action:          ActionRetry,
	})
	if err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if retried.Status != StatusWorkspacePreparing || retried.RetryStatus != "" ||
		retried.FailureCode != "" || retried.CurrentDispatchID == "" {
		t.Fatalf("retried = %#v", retried)
	}
}

func TestCreateValidationAndActiveJobConflict(t *testing.T) {
	repository, _, db, _, input := openWorkflowRepository(t, "DRAFT")
	defer db.Close()
	ctx := context.Background()

	if _, err := repository.Create(ctx, input); !errors.Is(err, ErrPlanNotReady) {
		t.Fatalf("draft plan error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE plans SET status = 'READY' WHERE id = 'plan-1'`); err != nil {
		t.Fatalf("ready plan: %v", err)
	}
	created, err := repository.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := repository.Create(ctx, input); !errors.Is(err, ErrActiveJob) {
		t.Fatalf("active duplicate error = %v", err)
	}
	if _, err := repository.Transition(ctx, created.ID, TransitionInput{
		ExpectedVersion: created.Version,
		Action:          ActionExecutionStarted,
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid transition error = %v", err)
	}

	unsafe := input
	unsafe.PlanID = "missing"
	if _, err := repository.Create(ctx, unsafe); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("missing plan error = %v", err)
	}
}

func TestProjectDeleteCascadesWorkflowAndTransitions(t *testing.T) {
	repository, _, db, _, input := openWorkflowRepository(t, "READY")
	defer db.Close()
	ctx := context.Background()

	job, err := repository.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := repository.Transition(ctx, job.ID, TransitionInput{ExpectedVersion: 1, Action: ActionStart}); err != nil {
		t.Fatalf("start error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM projects WHERE id = 'project-1'`); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	var jobsCount, transitionsCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_jobs WHERE id = ?`, job.ID).Scan(&jobsCount); err != nil {
		t.Fatalf("count workflow jobs: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_job_transitions WHERE workflow_job_id = ?`, job.ID).Scan(&transitionsCount); err != nil {
		t.Fatalf("count transitions: %v", err)
	}
	if jobsCount != 0 || transitionsCount != 0 {
		t.Fatalf("jobs=%d transitions=%d", jobsCount, transitionsCount)
	}
}

func TestTransitionDetailsHaveBoundedJSONSize(t *testing.T) {
	repository, _, db, _, input := openWorkflowRepository(t, "READY")
	defer db.Close()
	ctx := context.Background()

	job, err := repository.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_, err = repository.Transition(ctx, job.ID, TransitionInput{
		ExpectedVersion: 1,
		Action:          ActionStart,
		Details:         map[string]any{"value": strings.Repeat("x", 33*1024)},
	})
	if !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("oversized details error = %v", err)
	}
}
