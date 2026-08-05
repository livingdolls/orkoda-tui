package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/llmprovider"
)

type providerCatalogStub struct {
	items []llm.ProviderInfo
}

func (s providerCatalogStub) List() []llm.ProviderInfo {
	return append([]llm.ProviderInfo(nil), s.items...)
}

type providerAdminStub struct {
	saved llmprovider.SaveInput
}

func (s *providerAdminStub) Save(_ context.Context, name string, input llmprovider.SaveInput) (llm.ProviderInfo, error) {
	s.saved = input
	return llm.ProviderInfo{
		Name: name, DefaultModel: input.DefaultModel, BaseURL: input.BaseURL,
		Configured: true, CredentialStored: true, Source: "tui", Editable: true, Deletable: true,
	}, nil
}
func (s *providerAdminStub) Delete(context.Context, string) error { return nil }
func (s *providerAdminStub) Test(_ context.Context, name string) (llmprovider.TestResult, error) {
	return llmprovider.TestResult{Provider: name, Model: "model-a", LatencyMS: 12, ResponsePreview: "OK"}, nil
}

type policyReaderStub struct {
	info llm.PolicyInfo
}

func (s policyReaderStub) Info() llm.PolicyInfo { return s.info }

func TestLLMProviderRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := &providerAdminStub{}
	registerLLMRoutes(
		router.Group("/api/v1"),
		providerCatalogStub{items: []llm.ProviderInfo{{
			Name: "openrouter", DefaultModel: "example/model", Configured: true,
			StructuredOutput: true, Default: true,
		}}},
		admin,
		policyReaderStub{info: llm.PolicyInfo{
			AttemptTimeoutMS: 45000, MaxWallClockMS: 120000, MaxAttempts: 3,
			Fallbacks: []llm.FallbackTarget{{Provider: "local-fake", Model: "local-fake-planner-v1"}},
			Budget:    llm.TokenBudget{MaxTotalTokens: 60000}, RedactionMode: "strict",
			StructuredValidation: true, MaxRepairAttempts: 1, MaxStructuredResponseBytes: 1 << 20,
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

	response = performRequest(router, http.MethodPut, "/api/v1/llm/providers/deepseek", `{
        "base_url":"https://api.deepseek.com",
        "default_model":"deepseek-v4-flash",
        "api_key":"secret",
        "json_mode":"json_object"
    }`)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected save response: %d %s", response.Code, response.Body.String())
	}
	if admin.saved.APIKey != "secret" || admin.saved.DefaultModel != "deepseek-v4-flash" {
		t.Fatalf("unexpected saved input %#v", admin.saved)
	}

	response = performRequest(router, http.MethodPost, "/api/v1/llm/providers/deepseek/test", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"response_preview":"OK"`) {
		t.Fatalf("unexpected test response: %d %s", response.Code, response.Body.String())
	}

	response = performRequest(router, http.MethodDelete, "/api/v1/llm/providers/deepseek", "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("unexpected delete response: %d %s", response.Code, response.Body.String())
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
}
