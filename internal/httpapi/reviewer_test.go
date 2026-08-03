package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/reviewer"
)

type fakeReviewRegistry struct {
	run    reviewer.Run
	issues []reviewer.Issue
	err    error
}

func (f fakeReviewRegistry) Get(context.Context, string) (reviewer.Run, error) {
	if f.err != nil {
		return reviewer.Run{}, f.err
	}
	return f.run, nil
}

func (f fakeReviewRegistry) ListWorkflow(context.Context, string) ([]reviewer.Run, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []reviewer.Run{f.run}, nil
}

func (f fakeReviewRegistry) ListIssues(context.Context, string) ([]reviewer.Issue, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.issues, nil
}

func TestReviewRoutesReturnRunsAndIssues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := fakeReviewRegistry{
		run: reviewer.Run{
			ID: "review-1", WorkflowJobID: "workflow-1", Status: reviewer.StatusCompleted,
			Verdict: reviewer.VerdictRequestRevision,
		},
		issues: []reviewer.Issue{
			{ID: "issue-1", ReviewRunID: "review-1", Key: "bug", Blocking: true},
		},
	}
	router := gin.New()
	registerReviewRoutes(router.Group("/api/v1"), registry)
	for _, path := range []string{
		"/api/v1/jobs/workflow-1/reviews",
		"/api/v1/reviews/review-1",
		"/api/v1/reviews/review-1/issues",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d body=%s", path, response.Code, response.Body.String())
		}
		var payload struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || len(payload.Data) == 0 {
			t.Fatalf("GET %s payload = %s error=%v", path, response.Body.String(), err)
		}
	}
}

func TestReviewRoutesMapNotFoundAndUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name     string
		registry ReviewRegistry
		want     int
	}{
		{name: "not found", registry: fakeReviewRegistry{err: reviewer.ErrNotFound}, want: http.StatusNotFound},
		{name: "unavailable", registry: nil, want: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			registerReviewRoutes(router.Group("/api/v1"), test.registry)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/missing", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
