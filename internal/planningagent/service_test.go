package planningagent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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

func seedPlanningAgentState(
	t *testing.T,
	ctx context.Context,
	db interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	},
) (*plans.Repository, planningcontext.Context) {
	t.Helper()
	panic("implemented below")
}
