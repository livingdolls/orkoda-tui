package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/artifact"
)

func TestArtifactRouteServesPlainTextAndKeepsEnvelopeOutOfLogContent(t *testing.T) {
	store, err := artifact.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "checks/run-1/test.log", strings.NewReader("line one\nline two\n")); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api/v1")
	registerArtifactRoutes(api, store)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/checks/run-1/test.log", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if response.Body.String() != "line one\nline two\n" {
		t.Fatalf("artifact body = %q", response.Body.String())
	}

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/../secret", nil))
	if missing.Code != http.StatusNotFound && missing.Code != http.StatusBadRequest {
		t.Fatalf("traversal status = %d, body = %s", missing.Code, missing.Body.String())
	}
}
