package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
	"github.com/livingdolls/orkoda-tui/internal/repositorysummary"
)

type fakeSummaryRegistry struct {
	summary repositorysummary.Summary
	err     error
}

func (f fakeSummaryRegistry) Generate(context.Context, string) (repositorysummary.Summary, error) {
	return f.summary, f.err
}

func (f fakeSummaryRegistry) Current(context.Context, string) (repositorysummary.Summary, error) {
	return f.summary, f.err
}

type fakePlanningContextRegistry struct {
	planningContext planningcontext.Context
	err             error
}

func (f fakePlanningContextRegistry) Normalize(context.Context, string) (planningcontext.Context, error) {
	return f.planningContext, f.err
}

func (f fakePlanningContextRegistry) Current(context.Context, string) (planningcontext.Context, error) {
	return f.planningContext, f.err
}

func TestPlanningRoutesReturnStructuredData(t *testing.T) {
	summary := repositorysummary.Summary{
		ID:           "summary-1",
		RepositoryID: "repo-1",
		HeadSHA:      "head-1",
		Snapshot: repositorysummary.Snapshot{
			Languages: []string{"Go"},
		},
	}
	planningContext := planningcontext.Context{
		ID:                  "context-1",
		PlanID:              "plan-1",
		PlanVersion:         1,
		RepositorySummaryID: "summary-1",
		NormalizedPlan: planningcontext.NormalizedPlan{
			Goal: "Add blog",
		},
	}
	router := NewRouterWithServices("test", nil, nil, RouterServices{
		RepositorySummaries: fakeSummaryRegistry{summary: summary},
		PlanningContexts:    fakePlanningContextRegistry{planningContext: planningContext},
	})

	for _, testCase := range []struct {
		method string
		path   string
		status int
		id     string
	}{
		{http.MethodPost, "/api/v1/repositories/repo-1/summaries", http.StatusCreated, "summary-1"},
		{http.MethodGet, "/api/v1/repositories/repo-1/summaries/current", http.StatusOK, "summary-1"},
		{http.MethodPost, "/api/v1/plans/plan-1/normalize", http.StatusCreated, "context-1"},
		{http.MethodGet, "/api/v1/plans/plan-1/context", http.StatusOK, "context-1"},
	} {
		request := httptest.NewRequest(testCase.method, testCase.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != testCase.status {
			t.Fatalf("%s %s status = %d, want %d; body=%s", testCase.method, testCase.path, response.Code, testCase.status, response.Body.String())
		}
		var payload struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if payload.Data.ID != testCase.id {
			t.Fatalf("response ID = %q, want %q", payload.Data.ID, testCase.id)
		}
	}
}

func TestPlanningRoutesMapMissingPrerequisites(t *testing.T) {
	router := NewRouterWithServices("test", nil, nil, RouterServices{
		RepositorySummaries: fakeSummaryRegistry{err: repositorysummary.ErrNotFound},
		PlanningContexts:    fakePlanningContextRegistry{err: planningcontext.ErrSummaryMissing},
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/repositories/repo-1/summaries/current", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("summary status = %d, want 404", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/plans/plan-1/normalize", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("normalize status = %d, want 409", response.Code)
	}

	if !errors.Is(planningcontext.ErrSummaryMissing, planningcontext.ErrSummaryMissing) {
		t.Fatal("sentinel error should support errors.Is")
	}
}
