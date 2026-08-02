package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type fakeWorkflowJobRegistry struct {
	job             workflowjob.Job
	transitions     []workflowjob.Transition
	createdInput    workflowjob.CreateInput
	transitionJobID string
	transitionInput workflowjob.TransitionInput
	err             error
}

func (f *fakeWorkflowJobRegistry) Create(_ context.Context, input workflowjob.CreateInput) (workflowjob.Job, error) {
	if f.err != nil {
		return workflowjob.Job{}, f.err
	}
	f.createdInput = input
	return f.job, nil
}

func (f *fakeWorkflowJobRegistry) Get(context.Context, string) (workflowjob.Job, error) {
	if f.err != nil {
		return workflowjob.Job{}, f.err
	}
	return f.job, nil
}

func (f *fakeWorkflowJobRegistry) ListProject(context.Context, string) ([]workflowjob.Job, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []workflowjob.Job{f.job}, nil
}

func (f *fakeWorkflowJobRegistry) Transition(_ context.Context, jobID string, input workflowjob.TransitionInput) (workflowjob.Job, error) {
	if f.err != nil {
		return workflowjob.Job{}, f.err
	}
	f.transitionJobID = jobID
	f.transitionInput = input
	job := f.job
	job.Version++
	job.Status = workflowjob.StatusWorkspacePreparing
	return job, nil
}

func (f *fakeWorkflowJobRegistry) ListTransitions(context.Context, string) ([]workflowjob.Transition, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.transitions, nil
}

func testWorkflowJob() workflowjob.Job {
	now := time.Unix(100, 0).UTC()
	return workflowjob.Job{
		ID:            "workflow-1",
		ProjectID:     "project-1",
		PlanID:        "plan-1",
		PlanVersionID: "plan-version-1",
		RepositoryID:  "repository-1",
		BaseBranch:    "main",
		BaseCommitSHA: "abc123",
		Status:        workflowjob.StatusReady,
		Version:       1,
		Limits: workflowjob.Limits{
			MaxRevisions:     3,
			MaxStageAttempts: 3,
			MaxToolCalls:     120,
			WallClockSeconds: 3600,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func workflowRouter(registry WorkflowJobRegistry) http.Handler {
	return NewRouterWithServices("development", nil, nil, RouterServices{WorkflowJobs: registry})
}

func TestWorkflowJobCreateListAndReadRoutes(t *testing.T) {
	registry := &fakeWorkflowJobRegistry{
		job: testWorkflowJob(),
		transitions: []workflowjob.Transition{{
			Sequence:        1,
			WorkflowJobID:   "workflow-1",
			Action:          workflowjob.ActionCreate,
			ToStatus:        workflowjob.StatusReady,
			WorkflowVersion: 1,
			Details:         []byte(`{}`),
		}},
	}
	router := workflowRouter(registry)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/projects/project-1/jobs", strings.NewReader(`{
		"plan_id":"plan-1",
		"repository_id":"repository-1",
		"base_branch":"main",
		"limits":{"max_revisions":4,"max_stage_attempts":2,"max_tool_calls":80,"wall_clock_seconds":1800}
	}`))
	createRequest.Header.Set("content-type", "application/json")
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", createResponse.Code, createResponse.Body.String())
	}
	if registry.createdInput.ProjectID != "project-1" || registry.createdInput.PlanID != "plan-1" ||
		registry.createdInput.Limits.MaxRevisions != 4 {
		t.Fatalf("created input = %#v", registry.createdInput)
	}

	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-1/jobs", nil))
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"workflow-1"`) {
		t.Fatalf("list status = %d body = %s", listResponse.Code, listResponse.Body.String())
	}

	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/workflow-1", nil))
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"status":"READY"`) {
		t.Fatalf("get status = %d body = %s", getResponse.Code, getResponse.Body.String())
	}

	transitionsResponse := httptest.NewRecorder()
	router.ServeHTTP(transitionsResponse, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/workflow-1/transitions", nil))
	if transitionsResponse.Code != http.StatusOK || !strings.Contains(transitionsResponse.Body.String(), `"action":"CREATE"`) {
		t.Fatalf("transitions status = %d body = %s", transitionsResponse.Code, transitionsResponse.Body.String())
	}
}

func TestWorkflowActionRoutesUseExplicitTransition(t *testing.T) {
	registry := &fakeWorkflowJobRegistry{job: testWorkflowJob()}
	router := workflowRouter(registry)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/workflow-1/start", strings.NewReader(`{
		"expected_version":1,
		"details":{"requested_by":"local-user"}
	}`))
	request.Header.Set("content-type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("start status = %d body = %s", response.Code, response.Body.String())
	}
	if registry.transitionJobID != "workflow-1" || registry.transitionInput.Action != workflowjob.ActionStart ||
		registry.transitionInput.ExpectedVersion != 1 || registry.transitionInput.Details["requested_by"] != "local-user" {
		t.Fatalf("transition input = %#v", registry.transitionInput)
	}
}

func TestWorkflowJobErrorsMapToHTTPStatus(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "not found", err: workflowjob.ErrNotFound, status: http.StatusNotFound},
		{name: "project missing", err: workflowjob.ErrProjectNotFound, status: http.StatusNotFound},
		{name: "invalid", err: workflowjob.ErrInvalidJob, status: http.StatusBadRequest},
		{name: "plan not ready", err: workflowjob.ErrPlanNotReady, status: http.StatusConflict},
		{name: "active", err: workflowjob.ErrActiveJob, status: http.StatusConflict},
		{name: "version", err: workflowjob.ErrVersionConflict, status: http.StatusConflict},
		{name: "transition", err: workflowjob.ErrInvalidTransition, status: http.StatusConflict},
		{name: "revision", err: workflowjob.ErrRevisionLimit, status: http.StatusConflict},
		{name: "internal", err: errors.New("database unavailable"), status: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := &fakeWorkflowJobRegistry{job: testWorkflowJob(), err: test.err}
			response := httptest.NewRecorder()
			workflowRouter(registry).ServeHTTP(
				response,
				httptest.NewRequest(http.MethodGet, "/api/v1/jobs/workflow-1", nil),
			)
			if response.Code != test.status {
				t.Fatalf("status = %d want %d body = %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

func TestWorkflowRoutesRequireRegistryAndValidJSON(t *testing.T) {
	unavailable := httptest.NewRecorder()
	workflowRouter(nil).ServeHTTP(
		unavailable,
		httptest.NewRequest(http.MethodGet, "/api/v1/jobs/workflow-1", nil),
	)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d", unavailable.Code)
	}

	registry := &fakeWorkflowJobRegistry{job: testWorkflowJob()}
	invalid := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/workflow-1/start", strings.NewReader(`{"expected_version":`))
	request.Header.Set("content-type", "application/json")
	workflowRouter(registry).ServeHTTP(invalid, request)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d body = %s", invalid.Code, invalid.Body.String())
	}
}
