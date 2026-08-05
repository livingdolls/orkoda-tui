package config

import (
	"strings"
	"testing"
)

func TestLoadLLMConfigRegistersMultipleProviders(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", strings.Repeat("d", 32))
	t.Setenv("OPENAI_API_KEY", strings.Repeat("o", 32))
	t.Setenv("ORKODA_LLM_PROVIDER", "deepseek")
	t.Setenv("ORKODA_LLM_BASE_URL", "")
	t.Setenv("ORKODA_LLM_API_KEY", "")
	t.Setenv("ORKODA_LLM_MODEL", "")
	t.Setenv("ORKODA_LLM_PROVIDERS_JSON", `[
		{"name":"deepseek","base_url":"https://api.deepseek.example/v1","api_key_env":"DEEPSEEK_API_KEY","model":"deepseek-coder"},
		{"name":"openai","base_url":"https://api.openai.example/v1","api_key_env":"OPENAI_API_KEY","model":"review-model","timeout":"75s"}
	]`)
	t.Setenv("ORKODA_LLM_FALLBACKS_JSON", `[{"provider":"openai","model":"review-model"}]`)

	config, err := loadLLMConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Provider != "deepseek" || config.Model != "deepseek-coder" {
		t.Fatalf("unexpected default: %s/%s", config.Provider, config.Model)
	}
	if len(config.Providers) != 2 {
		t.Fatalf("expected two providers, got %d", len(config.Providers))
	}
	if config.Providers[1].Timeout.String() != "1m15s" {
		t.Fatalf("unexpected provider timeout: %s", config.Providers[1].Timeout)
	}
}

func TestLoadLLMConfigRejectsUnregisteredFallback(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", strings.Repeat("d", 32))
	t.Setenv("ORKODA_LLM_PROVIDER", "deepseek")
	t.Setenv("ORKODA_LLM_PROVIDERS_JSON", `[{"name":"deepseek","base_url":"https://api.deepseek.example/v1","api_key_env":"DEEPSEEK_API_KEY","model":"deepseek-coder"}]`)
	t.Setenv("ORKODA_LLM_FALLBACKS_JSON", `[{"provider":"missing","model":"model"}]`)
	if _, err := loadLLMConfig(); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected unregistered fallback error, got %v", err)
	}
}
