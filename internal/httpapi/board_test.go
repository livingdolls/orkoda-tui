package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type boardWorkflowRegistry struct {
	jobs []workflowjob.Job
}

func (r boardWorkflowRegistry) Create(context.Context, workflowjob.CreateInput) (workflowjob.Job, error) {
	return workflowjob.Job{}, nil
}

func (r boardWorkflowRegistry) Get(context.Context, string) (workflowjob.Job, error) {
	return workflowjob.Job{}, nil
}

func (r boardWorkflowRegistry) ListProject(context.Context, string) ([]workflowjob.Job, error) {
	return r.jobs, nil
}

func (r boardWorkflowRegistry) Transition(context.Context, string, workflowjob.TransitionInput) (workflowjob.Job, error) {
	return workflowjob.Job{}, nil
}

func (r boardWorkflowRegistry) ListTransitions(context.Context, string) ([]workflowjob.Transition, error) {
	return nil, nil
}

func TestBoardRouteReturnsWorkflowSummaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api/v1")
	registerWorkflowJobRoutes(api, boardWorkflowRegistry{jobs: make([]workflowjob.Job, 1)})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-1/board", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data struct {
			Jobs []workflowjob.Job `json:"jobs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data.Jobs) != 1 {
		t.Fatalf("unexpected jobs: %#v", payload.Data.Jobs)
	}
}
