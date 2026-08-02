package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
)

type fakeAgentSettingsRegistry struct {
	settings       agentconfig.Settings
	updatedProject string
	updateInput    agentconfig.UpdateInput
	operationError error
}

func (f *fakeAgentSettingsRegistry) Get(_ context.Context, _ string) (agentconfig.Settings, error) {
	if f.operationError != nil {
		return agentconfig.Settings{}, f.operationError
	}
	return f.settings, nil
}

func (f *fakeAgentSettingsRegistry) Update(_ context.Context, projectID string, input agentconfig.UpdateInput) (agentconfig.Settings, error) {
	if f.operationError != nil {
		return agentconfig.Settings{}, f.operationError
	}
	f.updatedProject = projectID
	f.updateInput = input
	return f.settings, nil
}

func testAgentSettings() agentconfig.Settings {
	now := time.Unix(100, 0).UTC()
	return agentconfig.Settings{
		ProjectID: "project-1",
		Version:   2,
		Agents: []agentconfig.AgentConfig{
			{Role: agentconfig.RolePlanner, Temperature: 0.1, MaxOutputTokens: 4096, Enabled: true},
			{Role: agentconfig.RoleExecutor, Temperature: 0.1, MaxOutputTokens: 8192, Enabled: true},
			{Role: agentconfig.RoleReviewer, Temperature: 0, MaxOutputTokens: 4096, Enabled: true},
		},
		ToolPolicies: []agentconfig.ToolPolicy{
			{
				Role: agentconfig.RolePlanner, AllowedTools: []string{}, AllowedCommandProfiles: []string{},
				NetworkAccess: agentconfig.NetworkDisabled, FilesystemAccess: agentconfig.FilesystemReadOnly,
				CommandTimeoutMS: 30000, MaxCommandOutputBytes: 262144, MaxFileBytes: 1048576, MaxPatchBytes: 1048576,
			},
			{
				Role: agentconfig.RoleExecutor, AllowedTools: []string{agentconfig.ToolFileRead}, AllowedCommandProfiles: []string{},
				NetworkAccess: agentconfig.NetworkDisabled, FilesystemAccess: agentconfig.FilesystemWorkspaceWrite,
				CommandTimeoutMS: 120000, MaxCommandOutputBytes: 1048576, MaxFileBytes: 2097152, MaxPatchBytes: 4194304,
			},
			{
				Role: agentconfig.RoleReviewer, AllowedTools: []string{agentconfig.ToolGitDiff}, AllowedCommandProfiles: []string{},
				NetworkAccess: agentconfig.NetworkDisabled, FilesystemAccess: agentconfig.FilesystemReadOnly,
				CommandTimeoutMS: 30000, MaxCommandOutputBytes: 262144, MaxFileBytes: 2097152, MaxPatchBytes: 4194304,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestAgentSettingsGetAndUpdateRoutes(t *testing.T) {
	registry := &fakeAgentSettingsRegistry{settings: testAgentSettings()}
	router := NewRouterWithServices("development", nil, nil, RouterServices{AgentSettings: registry})

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-1/agent-settings", nil)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"version":2`) {
		t.Fatalf("GET status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	if !strings.Contains(getResponse.Body.String(), `"network_access":"DISABLED"`) {
		t.Fatalf("GET body missing policy: %s", getResponse.Body.String())
	}

	body := `{
		"expected_version":2,
		"agents":[
			{"role":"PLANNER","provider":"","model":"","temperature":0.1,"max_output_tokens":4096,"enabled":true,"system_instruction":""},
			{"role":"EXECUTOR","provider":"openrouter","model":"example/model","temperature":0.1,"max_output_tokens":8192,"enabled":true,"system_instruction":""},
			{"role":"REVIEWER","provider":"","model":"","temperature":0,"max_output_tokens":4096,"enabled":true,"system_instruction":""}
		],
		"tool_policies":[
			{"role":"PLANNER","allowed_tools":[],"allowed_command_profiles":[],"network_access":"DISABLED","filesystem_access":"READ_ONLY","command_timeout_ms":30000,"max_command_output_bytes":262144,"max_file_bytes":1048576,"max_patch_bytes":1048576},
			{"role":"EXECUTOR","allowed_tools":["file_read"],"allowed_command_profiles":[],"network_access":"DISABLED","filesystem_access":"WORKSPACE_WRITE","command_timeout_ms":120000,"max_command_output_bytes":1048576,"max_file_bytes":2097152,"max_patch_bytes":4194304},
			{"role":"REVIEWER","allowed_tools":["git_diff"],"allowed_command_profiles":[],"network_access":"DISABLED","filesystem_access":"READ_ONLY","command_timeout_ms":30000,"max_command_output_bytes":262144,"max_file_bytes":2097152,"max_patch_bytes":4194304}
		]
	}`
	putRequest := httptest.NewRequest(http.MethodPut, "/api/v1/projects/project-1/agent-settings", strings.NewReader(body))
	putRequest.Header.Set("content-type", "application/json")
	putResponse := httptest.NewRecorder()
	router.ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", putResponse.Code, putResponse.Body.String())
	}
	if registry.updatedProject != "project-1" || registry.updateInput.ExpectedVersion != 2 {
		t.Fatalf("update project=%q input=%#v", registry.updatedProject, registry.updateInput)
	}
	if registry.updateInput.Agents[1].Provider != "openrouter" {
		t.Fatalf("executor config = %#v", registry.updateInput.Agents[1])
	}
}

func TestAgentSettingsErrorsMapToHTTPStatus(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "missing", err: agentconfig.ErrProjectNotFound, status: http.StatusNotFound},
		{name: "invalid", err: agentconfig.ErrInvalidSettings, status: http.StatusBadRequest},
		{name: "conflict", err: agentconfig.ErrVersionConflict, status: http.StatusConflict},
		{name: "internal", err: errors.New("database unavailable"), status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := &fakeAgentSettingsRegistry{operationError: test.err}
			router := NewRouterWithServices("development", nil, nil, RouterServices{AgentSettings: registry})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-1/agent-settings", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

func TestAgentSettingsRouteRejectsMalformedBodyAndUnavailableRegistry(t *testing.T) {
	router := NewRouterWithServices("development", nil, nil, RouterServices{})
	unavailableRequest := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-1/agent-settings", nil)
	unavailableResponse := httptest.NewRecorder()
	router.ServeHTTP(unavailableResponse, unavailableRequest)
	if unavailableResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d body=%s", unavailableResponse.Code, unavailableResponse.Body.String())
	}

	registry := &fakeAgentSettingsRegistry{settings: testAgentSettings()}
	router = NewRouterWithServices("development", nil, nil, RouterServices{AgentSettings: registry})
	badRequest := httptest.NewRequest(http.MethodPut, "/api/v1/projects/project-1/agent-settings", strings.NewReader(`{"expected_version":`))
	badRequest.Header.Set("content-type", "application/json")
	badResponse := httptest.NewRecorder()
	router.ServeHTTP(badResponse, badRequest)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("malformed status=%d body=%s", badResponse.Code, badResponse.Body.String())
	}
}
