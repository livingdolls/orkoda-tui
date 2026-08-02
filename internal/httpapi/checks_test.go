package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/checks"
)

type fakeCheckRegistry struct {
	run   checks.Run
	steps []checks.Step
	err   error
}

func (f fakeCheckRegistry) Get(context.Context, string) (checks.Run, error) {
	if f.err != nil {
		return checks.Run{}, f.err
	}
	return f.run, nil
}

func (f fakeCheckRegistry) ListWorkflow(context.Context, string) ([]checks.Run, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []checks.Run{f.run}, nil
}

func (f fakeCheckRegistry) ListSteps(context.Context, string) ([]checks.Step, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.steps, nil
}

func TestCheckRoutesReturnRunsAndSteps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := fakeCheckRegistry{
		run: checks.Run{ID: "check-1", WorkflowJobID: "workflow-1", Status: checks.StatusFailed},
		steps: []checks.Step{{
			ID: "step-1", CheckRunID: "check-1", Profile: "go.test", Status: checks.StatusFailed,
		}},
	}
	router := gin.New()
	registerCheckRoutes(router.Group("/api/v1"), registry)

	for _, path := range []string{
		"/api/v1/jobs/workflow-1/checks",
		"/api/v1/checks/check-1",
		"/api/v1/checks/check-1/steps",
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

func TestCheckRoutesMapNotFoundAndUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name     string
		registry CheckRegistry
		want     int
	}{
		{name: "not found", registry: fakeCheckRegistry{err: checks.ErrNotFound}, want: http.StatusNotFound},
		{name: "unavailable", registry: nil, want: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			registerCheckRoutes(router.Group("/api/v1"), test.registry)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/checks/missing", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
