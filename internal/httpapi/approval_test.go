package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/approval"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type fakeApprovalRegistry struct {
	kind    approval.Kind
	input   approval.DecideInput
	outcome approval.Outcome
	items   []approval.Decision
	err     error
}

func (f *fakeApprovalRegistry) Decide(
	_ context.Context,
	_ string,
	kind approval.Kind,
	input approval.DecideInput,
) (approval.Outcome, error) {
	f.kind = kind
	f.input = input
	if f.err != nil {
		return approval.Outcome{}, f.err
	}
	return f.outcome, nil
}

func (f *fakeApprovalRegistry) Get(context.Context, string) (approval.Decision, error) {
	if f.err != nil {
		return approval.Decision{}, f.err
	}
	if len(f.items) == 0 {
		return approval.Decision{}, approval.ErrNotFound
	}
	return f.items[0], nil
}

func (f *fakeApprovalRegistry) ListWorkflow(context.Context, string) ([]approval.Decision, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

func TestApprovalRoutesBindSnapshotAndDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := &fakeApprovalRegistry{outcome: approval.Outcome{
		Decision: approval.Decision{ID: "decision-1", Kind: approval.KindApprove},
		Workflow: workflowjob.Job{ID: "workflow-1", Status: workflowjob.StatusApproved},
	}}
	router := gin.New()
	registerApprovalRoutes(router.Group("/api/v1"), registry)
	body := []byte(`{
		"expected_version":8,
		"execution_version":1,
		"base_commit_sha":"abc123",
		"patch_checksum":"sha256:patch",
		"note":"reviewed",
		"review_override":true
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/workflow-1/approve", bytes.NewReader(body))
	request.Header.Set("content-type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if registry.kind != approval.KindApprove || registry.input.PatchChecksum != "sha256:patch" ||
		!registry.input.ReviewOverride {
		t.Fatalf("kind=%s input=%#v", registry.kind, registry.input)
	}
	var payload struct {
		Data approval.Outcome `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Decision.ID != "decision-1" || payload.Data.Workflow.Status != workflowjob.StatusApproved {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestApprovalRoutesListAndMapConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := &fakeApprovalRegistry{items: []approval.Decision{{ID: "decision-1"}}}
	router := gin.New()
	registerApprovalRoutes(router.Group("/api/v1"), registry)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/workflow-1/decisions", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}

	registry.err = approval.ErrBindingMismatch
	request = httptest.NewRequest(http.MethodPost, "/api/v1/jobs/workflow-1/reject", bytes.NewReader([]byte(`{
		"expected_version":8,
		"execution_version":1,
		"base_commit_sha":"abc",
		"patch_checksum":"sha256:patch",
		"note":"not acceptable"
	}`)))
	request.Header.Set("content-type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestApprovalRoutesRequireRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerApprovalRoutes(router.Group("/api/v1"), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/workflow-1/decisions", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
