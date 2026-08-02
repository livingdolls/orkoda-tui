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

type policyReaderStub struct {
	info llm.PolicyInfo
}

func (s policyReaderStub) Info() llm.PolicyInfo { return s.info }

func TestLLMProviderRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerLLMRoutes(
		router.Group("/api/v1"),
		providerCatalogStub{items: []llm.ProviderInfo{{
			Name:             "openrouter",
			DefaultModel:     "example/model",
			Configured:       true,
			StructuredOutput: true,
			Default:          true,
		}}},
		policyReaderStub{info: llm.PolicyInfo{
			AttemptTimeoutMS: 45000,
			MaxWallClockMS:   120000,
			MaxAttempts:      3,
			Fallbacks:        []llm.FallbackTarget{{Provider: "local-fake", Model: "local-fake-planner-v1"}},
			Budget:           llm.TokenBudget{MaxTotalTokens: 60000},
		}},
	)

	response := performRequest(router, http.MethodGet, "/api/v1/llm/providers", "")
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected provider response: %d %s", response.Code, response.Body.String())
	}
	var providersPayload struct {
		Data []llm.ProviderInfo `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &providersPayload); err != nil {
		t.Fatal(err)
	}
	if len(providersPayload.Data) != 1 || providersPayload.Data[0].Name != "openrouter" || !providersPayload.Data[0].Default {
		t.Fatalf("unexpected provider payload %#v", providersPayload.Data)
	}

	response = performRequest(router, http.MethodGet, "/api/v1/llm/policy", "")
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected policy response: %d %s", response.Code, response.Body.String())
	}
	var policyPayload struct {
		Data llm.PolicyInfo `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &policyPayload); err != nil {
		t.Fatal(err)
	}
	if policyPayload.Data.MaxAttempts != 3 || policyPayload.Data.Budget.MaxTotalTokens != 60000 {
		t.Fatalf("unexpected policy payload %#v", policyPayload.Data)
	}
	if len(policyPayload.Data.Fallbacks) != 1 || policyPayload.Data.Fallbacks[0].Provider != "local-fake" {
		t.Fatalf("unexpected policy fallbacks %#v", policyPayload.Data.Fallbacks)
	}
}
