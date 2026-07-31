package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/projects"
)

type fakeProjectRegistry struct {
	projects       []projects.Project
	createdName    string
	createdPath    string
	renamedID      string
	renamedName    string
	deletedID      string
	refreshedID    string
	operationError error
}

func (f *fakeProjectRegistry) Create(_ context.Context, name, repositoryPath string) (projects.Project, error) {
	if f.operationError != nil {
		return projects.Project{}, f.operationError
	}
	f.createdName = name
	f.createdPath = repositoryPath
	return f.projects[0], nil
}

func (f *fakeProjectRegistry) List(context.Context) ([]projects.Project, error) {
	if f.operationError != nil {
		return nil, f.operationError
	}
	return f.projects, nil
}

func (f *fakeProjectRegistry) Get(_ context.Context, projectID string) (projects.Project, error) {
	if f.operationError != nil {
		return projects.Project{}, f.operationError
	}
	for _, project := range f.projects {
		if project.ID == projectID {
			return project, nil
		}
	}
	return projects.Project{}, projects.ErrNotFound
}

func (f *fakeProjectRegistry) Rename(_ context.Context, projectID, name string) (projects.Project, error) {
	if f.operationError != nil {
		return projects.Project{}, f.operationError
	}
	f.renamedID = projectID
	f.renamedName = name
	project := f.projects[0]
	project.Name = name
	return project, nil
}

func (f *fakeProjectRegistry) Delete(_ context.Context, projectID string) error {
	if f.operationError != nil {
		return f.operationError
	}
	f.deletedID = projectID
	return nil
}

func (f *fakeProjectRegistry) Refresh(_ context.Context, projectID string) (projects.Project, error) {
	if f.operationError != nil {
		return projects.Project{}, f.operationError
	}
	f.refreshedID = projectID
	return f.projects[0], nil
}

func testProject() projects.Project {
	now := time.Unix(100, 0).UTC()
	return projects.Project{
		ID:   "project-1",
		Name: "Example",
		Repositories: []projects.RepositoryInfo{
			{
				ID:            "repository-1",
				ProjectID:     "project-1",
				LocalPath:     "/tmp/example",
				CurrentBranch: "main",
				HeadSHA:       "abc123",
				CreatedAt:     now,
				UpdatedAt:     now,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestCreateAndListProjects(t *testing.T) {
	registry := &fakeProjectRegistry{projects: []projects.Project{testProject()}}
	router := NewRouter("development", nil, registry)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(`{
		"name":"Example",
		"repository_path":"/tmp/example"
	}`))
	createRequest.Header.Set("content-type", "application/json")
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createResponse.Code, createResponse.Body.String())
	}
	if registry.createdName != "Example" || registry.createdPath != "/tmp/example" {
		t.Fatalf("create args name=%q path=%q", registry.createdName, registry.createdPath)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"head_sha":"abc123"`) {
		t.Fatalf("list status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectMutationEndpoints(t *testing.T) {
	registry := &fakeProjectRegistry{projects: []projects.Project{testProject()}}
	router := NewRouter("development", nil, registry)

	renameRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/project-1", strings.NewReader(`{"name":"Renamed"}`))
	renameRequest.Header.Set("content-type", "application/json")
	renameResponse := httptest.NewRecorder()
	router.ServeHTTP(renameResponse, renameRequest)
	if renameResponse.Code != http.StatusOK || registry.renamedName != "Renamed" {
		t.Fatalf("rename status=%d body=%s", renameResponse.Code, renameResponse.Body.String())
	}

	refreshRequest := httptest.NewRequest(http.MethodPost, "/api/v1/projects/project-1/refresh", nil)
	refreshResponse := httptest.NewRecorder()
	router.ServeHTTP(refreshResponse, refreshRequest)
	if refreshResponse.Code != http.StatusOK || registry.refreshedID != "project-1" {
		t.Fatalf("refresh status=%d body=%s", refreshResponse.Code, refreshResponse.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/project-1", nil)
	deleteResponse := httptest.NewRecorder()
	router.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent || registry.deletedID != "project-1" {
		t.Fatalf("delete status=%d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
}

func TestProjectErrorsMapToHTTPStatus(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid", err: projects.ErrInvalidProject, status: http.StatusBadRequest},
		{name: "duplicate", err: projects.ErrRepositoryAlreadyRegistered, status: http.StatusConflict},
		{name: "not found", err: projects.ErrNotFound, status: http.StatusNotFound},
		{name: "internal", err: errors.New("database unavailable"), status: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := &fakeProjectRegistry{projects: []projects.Project{testProject()}, operationError: test.err}
			router := NewRouter("development", nil, registry)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}
