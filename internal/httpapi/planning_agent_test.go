package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/planningagent"
)

type planningAgentStub struct {
	run             planningagent.Run
	startErr        error
	currentErr      error
	getErr          error
	answerErr       error
	answers         []planningagent.AnswerInput
	startedPlan     string
	startedProvider string
	startedModel    string
}

func (s *planningAgentStub) Start(
	_ context.Context,
	planID, provider, model string,
) (planningagent.Run, error) {
	s.startedPlan = planID
	s.startedProvider = provider
	s.startedModel = model
	return s.run, s.startErr
}

func (s *planningAgentStub) Current(context.Context, string) (planningagent.Run, error) {
	return s.run, s.currentErr
}

func (s *planningAgentStub) Get(context.Context, string) (planningagent.Run, error) {
	return s.run, s.getErr
}

func (s *planningAgentStub) Answer(
	_ context.Context,
	_ string,
	answers []planningagent.AnswerInput,
) (planningagent.Run, error) {
	s.answers = answers
	return s.run, s.answerErr
}

func TestPlanningAgentRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &planningAgentStub{run: planningagent.Run{
		ID:        "run-1",
		PlanID:    "plan-1",
		Status:    planningagent.RunStatusNeedsInput,
		Questions: []planningagent.Question{},
	}}
	router := gin.New()
	registerPlanningAgentRoutes(router.Group("/api/v1"), stub, "openrouter", "example/model")

	response := performRequest(router, http.MethodPost, "/api/v1/plans/plan-1/planning-runs", `{}`)
	if response.Code != http.StatusCreated || stub.startedPlan != "plan-1" {
		t.Fatalf("unexpected start response: %d %s", response.Code, response.Body.String())
	}
	if stub.startedProvider != "openrouter" || stub.startedModel != "example/model" {
		t.Fatalf("unexpected defaults provider=%q model=%q", stub.startedProvider, stub.startedModel)
	}

	response = performRequest(
		router,
		http.MethodPost,
		"/api/v1/plans/plan-1/planning-runs",
		`{"provider":"custom","model":"custom-model"}`,
	)
	if response.Code != http.StatusCreated || stub.startedProvider != "custom" || stub.startedModel != "custom-model" {
		t.Fatalf("unexpected explicit provider response: %d provider=%q model=%q", response.Code, stub.startedProvider, stub.startedModel)
	}

	response = performRequest(router, http.MethodGet, "/api/v1/plans/plan-1/planning-runs/current", "")
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected current response: %d %s", response.Code, response.Body.String())
	}

	response = performRequest(
		router,
		http.MethodPost,
		"/api/v1/planning-runs/run-1/answers",
		`{"answers":[{"question_id":"question-1","answer":"content/blog"}]}`,
	)
	if response.Code != http.StatusCreated || len(stub.answers) != 1 {
		t.Fatalf("unexpected answer response: %d %s answers=%#v", response.Code, response.Body.String(), stub.answers)
	}
	if stub.answers[0].QuestionID != "question-1" || stub.answers[0].Answer != "content/blog" {
		t.Fatalf("unexpected decoded answers: %#v", stub.answers)
	}
}

func TestPlanningAgentRouteErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &planningAgentStub{startErr: planningagent.ErrActiveRun}
	router := gin.New()
	registerPlanningAgentRoutes(router.Group("/api/v1"), stub, "local-fake", "local-fake-planner-v1")

	response := performRequest(router, http.MethodPost, "/api/v1/plans/plan-1/planning-runs", `{}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected conflict, got %d %s", response.Code, response.Body.String())
	}

	stub.startErr = nil
	stub.currentErr = planningagent.ErrRunNotFound
	response = performRequest(router, http.MethodGet, "/api/v1/plans/plan-1/planning-runs/current", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected not found, got %d %s", response.Code, response.Body.String())
	}

	stub.currentErr = nil
	stub.answerErr = errors.Join(planningagent.ErrInvalidAnswers, errors.New("missing answer"))
	response = performRequest(router, http.MethodPost, "/api/v1/planning-runs/run-1/answers", `{"answers":[]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d %s", response.Code, response.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] == nil {
		t.Fatalf("expected structured error payload: %#v", payload)
	}
}

func performRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("content-type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
