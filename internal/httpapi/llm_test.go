package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/llm"
)

type providerCatalogStub struct {
	items []llm.ProviderInfo
}

func (s providerCatalogStub) List() []llm.ProviderInfo {
	return append([]llm.ProviderInfo(nil), s.items...)
}

func TestLLMProviderRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerLLMRoutes(router.Group("/api/v1"), providerCatalogStub{items: []llm.ProviderInfo{{
		Name:             "openrouter",
		DefaultModel:     "example/model",
		Configured:       true,
		StructuredOutput: true,
		Default:          true,
	}}})

	response := performRequest(router, http.MethodGet, "/api/v1/llm/providers", "")
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected provider response: %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []llm.ProviderInfo `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Name != "openrouter" || !payload.Data[0].Default {
		t.Fatalf("unexpected provider payload %#v", payload.Data)
	}
	if response.Body.String() == "" || json.Valid(response.Body.Bytes()) == false {
		t.Fatalf("expected valid JSON response %s", response.Body.String())
	}
}
