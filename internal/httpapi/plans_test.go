package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/plans"
)

type fakePlanRegistry struct {
	createdProjectID string
	createdTitle     string
	createdInput     plans.VersionInput
	plan             plans.Plan
	list             []plans.Plan
	err              error
}

func (f *fakePlanRegistry) Create(_ context.Context, projectID, title string, input plans.VersionInput) (plans.Plan, error) {
	f.createdProjectID = projectID
	f.createdTitle = title
	f.createdInput = input
	return f.plan, f.err
}

func (f *fakePlanRegistry) AddVersion(_ context.Context, _ string, _ plans.VersionInput) (plans.Plan, error) {
	return f.plan, f.err
}

func (f *fakePlanRegistry) ListProject(_ context.Context, _ string) ([]plans.Plan, error) {
	return f.list, f.err
}

func (f *fakePlanRegistry) Get(_ context.Context, _ string) (plans.Plan, error) {
	return f.plan, f.err
}

func (f *fakePlanRegistry) Update(_ context.Context, _ string, _ string, _ plans.Status) (plans.Plan, error) {
	return f.plan, f.err
}

func (f *fakePlanRegistry) Delete(_ context.Context, _ string) error {
	return f.err
}

func TestCreatePlanRoute(t *testing.T) {
	registry := &fakePlanRegistry{plan: samplePlan()}
	router := NewRouter("development", &fakeEventReader{}, nil, registry)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects/project-1/plans", strings.NewReader(`{
		"title":"Blog",
		"requirement":"Build a blog",
		"acceptance_criteria":["Lists articles"],
		"constraints":["Use existing stack"]
	}`))
	request.Header.Set("content-type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if registry.createdProjectID != "project-1" || registry.createdTitle != "Blog" {
		t.Fatalf("create args project=%q title=%q", registry.createdProjectID, registry.createdTitle)
	}
	if len(registry.createdInput.AcceptanceCriteria) != 1 {
		t.Fatalf("input = %#v", registry.createdInput)
	}
	if !strings.Contains(response.Body.String(), `"current_version":1`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestPlanRoutesMapValidationAndNotFound(t *testing.T) {
	registry := &fakePlanRegistry{err: plans.ErrInvalidPlan}
	router := NewRouter("development", &fakeEventReader{}, nil, registry)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects/project-1/plans", strings.NewReader(`{"title":"","requirement":""}`))
	request.Header.Set("content-type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("validation status = %d", response.Code)
	}

	registry.err = plans.ErrNotFound
	request = httptest.NewRequest(http.MethodGet, "/api/v1/plans/missing", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d", response.Code)
	}
}

func TestPlanRoutesRequireRegistry(t *testing.T) {
	router := NewRouter("development", &fakeEventReader{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-1/plans", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestPlanListAndDeleteRoutes(t *testing.T) {
	registry := &fakePlanRegistry{list: []plans.Plan{samplePlan()}}
	router := NewRouter("development", &fakeEventReader{}, nil, registry)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-1/plans", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"title":"Blog"`) {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/v1/plans/plan-1", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", response.Code)
	}
}

func TestPlanUnexpectedErrorReturnsInternalServerError(t *testing.T) {
	registry := &fakePlanRegistry{err: errors.New("database unavailable")}
	router := NewRouter("development", &fakeEventReader{}, nil, registry)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/plans/plan-1", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
}

func samplePlan() plans.Plan {
	now := time.Unix(1, 0).UTC()
	return plans.Plan{
		ID:             "plan-1",
		ProjectID:      "project-1",
		Title:          "Blog",
		Status:         plans.StatusDraft,
		CurrentVersion: 1,
		CreatedAt:      now,
		UpdatedAt:      now,
		Versions: []plans.Version{{
			ID:          "version-1",
			PlanID:      "plan-1",
			Version:     1,
			Requirement: "Build a blog",
			CreatedAt:   now,
		}},
	}
}
