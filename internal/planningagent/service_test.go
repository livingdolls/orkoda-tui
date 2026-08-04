package planningagent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
	"github.com/livingdolls/orkoda-tui/internal/plans"
)

type mutableContextReader struct {
	value planningcontext.Context
	err   error
}

type cancellingGateway struct {
	cancel context.CancelFunc
}

func (g cancellingGateway) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	g.cancel()
	return llm.Response{}, context.Canceled
}

func (r *mutableContextReader) Current(context.Context, string) (planningcontext.Context, error) {
	return r.value, r.err
}

func TestPlanningAgentQuestionFlow(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	planRepository, planningContext := seedPlanningAgentState(t, ctx, db)
	contextReader := &mutableContextReader{value: planningContext}
	provider, err := llm.NewFakeProvider("fake",
		llm.FakeResult{Response: llm.Response{
			ID: "response-questions",
			Content: `{
				"summary":"More information is required.",
				"steps":[],
				"open_questions":["Which directory should store Markdown files?"],
				"risks":[]
			}`,
			Usage: llm.Usage{InputTokens: 100, OutputTokens: 25},
		}},
		llm.FakeResult{Response: llm.Response{
			ID: "response-plan",
			Content: `{
				"summary":"Implement the Markdown blog.",
				"steps":[{
					"id":"step-1",
					"title":"Add content loader",
					"description":"Load Markdown files from the selected directory.",
					"affected_files":["internal/blog/loader.go"],
					"acceptance_criteria":["Articles are loaded deterministically"]
				}],
				"open_questions":[],
				"risks":[]
			}`,
			Usage: llm.Usage{InputTokens: 140, OutputTokens: 60},
		}},
		llm.FakeResult{Response: llm.Response{
			ID: "response-rerun",
			Content: `{
				"summary":"Implement the Markdown blog again.",
				"steps":[{
					"id":"step-1",
					"title":"Add content loader",
					"description":"Load Markdown files from the selected directory.",
					"affected_files":["internal/blog/loader.go"],
					"acceptance_criteria":["Articles are loaded deterministically"]
				}],
				"open_questions":[],
				"risks":[]
			}`,
			Usage: llm.Usage{InputTokens: 140, OutputTokens: 60},
		}},
		llm.FakeResult{Response: llm.Response{
			ID: "response-rerun-question",
			Content: `{
				"summary":"More information is required again.",
				"steps":[],
				"open_questions":["Which directory should store Markdown files?"],
				"risks":[]
			}`,
			Usage: llm.Usage{InputTokens: 140, OutputTokens: 25},
		}},
		llm.FakeResult{Response: llm.Response{
			ID: "response-auto-resolved",
			Content: `{
				"summary":"Automatically reused the previous answer.",
				"steps":[{
					"id":"step-1",
					"title":"Add content loader",
					"description":"Load Markdown files from the selected directory.",
					"affected_files":["internal/blog/loader.go"],
					"acceptance_criteria":["Articles are loaded deterministically"]
				}],
				"open_questions":[],
				"risks":[]
			}`,
			Usage: llm.Usage{InputTokens: 140, OutputTokens: 60},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := llm.NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llm.NewGateway(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db, contextReader, planRepository, gateway, nil)
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.Start(ctx, planningContext.PlanID, "fake", "fake-planner-v1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != RunStatusNeedsInput || len(first.Questions) != 1 {
		t.Fatalf("expected question run, got %#v", first)
	}
	if _, err := service.Start(ctx, planningContext.PlanID, "fake", "fake-planner-v1"); !errors.Is(err, ErrActiveRun) {
		t.Fatalf("expected active run error, got %v", err)
	}
	storedPlan, err := planRepository.Get(ctx, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if storedPlan.Status != plans.StatusNeedsInput {
		t.Fatalf("expected NEEDS_INPUT plan, got %s", storedPlan.Status)
	}

	completed, err := service.Answer(ctx, first.ID, []AnswerInput{{
		QuestionID: first.Questions[0].ID,
		Answer:     "Store them under content/blog.",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != RunStatusCompleted || completed.ParentRunID != first.ID {
		t.Fatalf("expected completed child run, got %#v", completed)
	}
	if completed.Result == nil || len(completed.Result.Steps) != 1 {
		t.Fatalf("expected persisted implementation plan, got %#v", completed.Result)
	}
	previous, err := service.Get(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Status != RunStatusSuperseded || previous.Questions[0].Status != QuestionStatusAnswered {
		t.Fatalf("expected answered superseded run, got %#v", previous)
	}
	storedPlan, err = planRepository.Get(ctx, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if storedPlan.Status != plans.StatusReady {
		t.Fatalf("expected READY plan, got %s", storedPlan.Status)
	}
	current, err := service.Current(ctx, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != completed.ID {
		t.Fatalf("expected current run %s, got %s", completed.ID, current.ID)
	}

	rerun, err := service.Start(ctx, planningContext.PlanID, "fake", "fake-planner-v1")
	if err != nil {
		t.Fatal(err)
	}
	if rerun.Status != RunStatusCompleted || rerun.Result == nil || len(rerun.Result.OpenQuestions) != 0 {
		t.Fatalf("expected rerun to reuse the answered question, got %#v", rerun)
	}
	requests := provider.Requests()
	if len(requests) != 3 || !strings.Contains(requests[2].Messages[1].Content, "Store them under content/blog.") {
		t.Fatalf("rerun did not include the persisted answer: %#v", requests)
	}

	duplicate, err := service.Start(ctx, planningContext.PlanID, "fake", "fake-planner-v1")
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Status != RunStatusNeedsInput {
		t.Fatalf("expected duplicate question run, got %#v", duplicate)
	}
	autoResolved, err := service.Start(ctx, planningContext.PlanID, "fake", "fake-planner-v1")
	if err != nil {
		t.Fatal(err)
	}
	if autoResolved.Status != RunStatusCompleted || autoResolved.ParentRunID != duplicate.ID {
		t.Fatalf("expected active duplicate to be auto-resolved, got %#v", autoResolved)
	}
	requests = provider.Requests()
	if len(requests) != 5 || !strings.Contains(requests[4].Messages[1].Content, "Store them under content/blog.") {
		t.Fatalf("auto-resolved run did not include the persisted answer: %#v", requests)
	}
}

func TestPlanningAgentRejectsStaleQuestionAnswers(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "stale.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	planRepository, planningContext := seedPlanningAgentState(t, ctx, db)
	contextReader := &mutableContextReader{value: planningContext}
	provider, err := llm.NewFakeProvider("fake", llm.FakeResult{Response: llm.Response{
		Content: `{"summary":"Need input","steps":[],"open_questions":["Which API?"],"risks":[]}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := llm.NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llm.NewGateway(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db, contextReader, planRepository, gateway, nil)
	if err != nil {
		t.Fatal(err)
	}

	run, err := service.Start(ctx, planningContext.PlanID, "fake", "fake-planner-v1")
	if err != nil {
		t.Fatal(err)
	}
	contextReader.value.ID = "new-context"
	_, err = service.Answer(ctx, run.ID, []AnswerInput{{QuestionID: run.Questions[0].ID, Answer: "REST"}})
	if !errors.Is(err, ErrStaleRun) {
		t.Fatalf("expected stale run error, got %v", err)
	}
}

func TestPlanningAgentPersistsFailureAfterRequestCancellation(t *testing.T) {
	background := context.Background()
	db, err := database.Open(background, filepath.Join(t.TempDir(), "cancelled.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(background, db); err != nil {
		t.Fatal(err)
	}

	planRepository, planningContext := seedPlanningAgentState(t, background, db)
	runCtx, cancel := context.WithCancel(background)
	service, err := NewService(
		db,
		&mutableContextReader{value: planningContext},
		planRepository,
		cancellingGateway{cancel: cancel},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Start(runCtx, planningContext.PlanID, "fake", "fake-model"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled planning request, got %v", err)
	}
	current, err := service.Current(background, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status == RunStatusRunning {
		t.Fatalf("cancelled request left planning run active: %#v", current)
	}
	plan, err := planRepository.Get(background, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != plans.StatusDraft {
		t.Fatalf("expected retryable DRAFT plan, got %s", plan.Status)
	}
}

func TestRecoverInterruptedPlanningRuns(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	planRepository, planningContext := seedPlanningAgentState(t, ctx, db)
	service, err := NewService(
		db,
		&mutableContextReader{value: planningContext},
		planRepository,
		cancellingGateway{cancel: func() {}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.createRun(ctx, planningContext, "", "fake", "fake-model")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planRepository.Get(ctx, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planRepository.Update(ctx, plan.ID, plan.Title, plans.StatusPlanning); err != nil {
		t.Fatal(err)
	}

	recovered, err := service.RecoverInterruptedRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("expected one recovered run, got %d", recovered)
	}
	storedRun, err := service.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedRun.Status != RunStatusCancelled || storedRun.ErrorCode != llm.ErrorCancelled {
		t.Fatalf("expected cancelled interrupted run, got %#v", storedRun)
	}
	storedPlan, err := planRepository.Get(ctx, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if storedPlan.Status != plans.StatusDraft {
		t.Fatalf("expected recovered DRAFT plan, got %s", storedPlan.Status)
	}
	secondRecovery, err := service.RecoverInterruptedRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if secondRecovery != 0 {
		t.Fatalf("expected idempotent recovery, got %d", secondRecovery)
	}
}

func seedPlanningAgentState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) (*plans.Repository, planningcontext.Context) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)
	`, "project-1", "Example", now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO repositories (
			id, project_id, local_path, current_branch, head_sha, remote_url,
			dirty, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)
	`, "repository-1", "project-1", "/tmp/example", "main", "abc123", "", now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO repository_summaries (id, repository_id, head_sha, summary_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, "summary-1", "repository-1", "abc123", `{}`, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}

	planRepository, err := plans.NewRepository(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planRepository.Create(ctx, "project-1", "Add Markdown blog", plans.VersionInput{
		Requirement:        "Add a Markdown blog.",
		AcceptanceCriteria: []string{"Articles can be listed"},
		Constraints:        []string{"Use the existing stack"},
	})
	if err != nil {
		t.Fatal(err)
	}
	normalized := planningcontext.NormalizedPlan{
		Goal:               plan.Title,
		Summary:            "Add a Markdown blog.",
		Scope:              []string{"Load Markdown articles"},
		NonGoals:           []string{},
		AcceptanceCriteria: []string{"Articles can be listed"},
		Constraints:        []string{"Use the existing stack"},
		AffectedAreas:      []string{"backend"},
		Risks:              []string{},
		OpenQuestions:      []string{"Which directory should store Markdown files?"},
		Repository: planningcontext.RepositoryContext{
			RepositoryID:    "repository-1",
			SummaryID:       "summary-1",
			HeadSHA:         "abc123",
			Languages:       []string{"Go"},
			Frameworks:      []string{},
			PackageManagers: []string{"Go Modules"},
			ImportantFiles:  []string{"go.mod"},
		},
	}
	normalizedJSON, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	planningContext := planningcontext.Context{
		ID:                  "context-1",
		PlanID:              plan.ID,
		PlanVersionID:       plan.Versions[0].ID,
		PlanVersion:         1,
		RepositorySummaryID: "summary-1",
		NormalizedPlan:      normalized,
		CreatedAt:           now,
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO planning_contexts (
			id, plan_id, plan_version_id, repository_summary_id, normalized_plan_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, planningContext.ID, planningContext.PlanID, planningContext.PlanVersionID,
		planningContext.RepositorySummaryID, string(normalizedJSON), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	return planRepository, planningContext
}
