package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/workspace"
)

type fakeWorkspaceRegistry struct {
	item  workspace.Workspace
	items []workspace.Workspace
	err   error
}

func (f *fakeWorkspaceRegistry) GetByWorkflow(context.Context, string) (workspace.Workspace, error) {
	if f.err != nil {
		return workspace.Workspace{}, f.err
	}
	return f.item, nil
}

func (f *fakeWorkspaceRegistry) ListProject(context.Context, string) ([]workspace.Workspace, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

func TestWorkspaceRoutesReadPersistedState(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	item := workspace.Workspace{
		ID: "workspace-1", WorkflowJobID: "workflow-1", ProjectID: "project-1",
		RepositoryID: "repository-1", Path: "/tmp/workspace", BaseCommitSHA: "abc123",
		HeadSHA: "abc123", Status: workspace.StatusReady, CreatedAt: now, UpdatedAt: now,
	}
	registry := &fakeWorkspaceRegistry{item: item, items: []workspace.Workspace{item}}
	router := NewRouterWithServices("development", nil, nil, RouterServices{Workspaces: registry})

	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/workflow-1/workspace", nil))
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"status":"READY"`) {
		t.Fatalf("get status = %d body = %s", getResponse.Code, getResponse.Body.String())
	}

	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-1/workspaces", nil))
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"workspace-1"`) {
		t.Fatalf("list status = %d body = %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestWorkspaceRoutesMapNotFoundAndUnavailable(t *testing.T) {
	notFound := httptest.NewRecorder()
	router := NewRouterWithServices("development", nil, nil, RouterServices{
		Workspaces: &fakeWorkspaceRegistry{err: workspace.ErrNotFound},
	})
	router.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/missing/workspace", nil))
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d", notFound.Code)
	}

	unavailable := httptest.NewRecorder()
	NewRouterWithServices("development", nil, nil, RouterServices{}).ServeHTTP(
		unavailable,
		httptest.NewRequest(http.MethodGet, "/api/v1/jobs/workflow-1/workspace", nil),
	)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d", unavailable.Code)
	}

	internal := httptest.NewRecorder()
	NewRouterWithServices("development", nil, nil, RouterServices{
		Workspaces: &fakeWorkspaceRegistry{err: errors.New("database unavailable")},
	}).ServeHTTP(internal, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/workflow-1/workspace", nil))
	if internal.Code != http.StatusInternalServerError {
		t.Fatalf("internal status = %d", internal.Code)
	}
}
