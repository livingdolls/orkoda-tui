package config

import (
	"testing"
	"time"
)

func clearLLMPolicyEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ORKODA_LLM_PROVIDER",
		"ORKODA_LLM_BASE_URL",
		"ORKODA_LLM_API_KEY",
		"ORKODA_LLM_MODEL",
		"ORKODA_LLM_JSON_MODE",
		"ORKODA_LLM_TIMEOUT",
		"ORKODA_LLM_HEADERS_JSON",
		"ORKODA_LLM_ATTEMPT_TIMEOUT",
		"ORKODA_LLM_MAX_WALL_CLOCK",
		"ORKODA_LLM_MAX_ATTEMPTS",
		"ORKODA_LLM_BACKOFF_INITIAL",
		"ORKODA_LLM_BACKOFF_MAX",
		"ORKODA_LLM_BACKOFF_JITTER",
		"ORKODA_LLM_MAX_INPUT_TOKENS",
		"ORKODA_LLM_MAX_OUTPUT_TOKENS",
		"ORKODA_LLM_MAX_TOTAL_TOKENS",
		"ORKODA_LLM_FALLBACKS_JSON",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadLLMPolicyDefaults(t *testing.T) {
	clearLLMPolicyEnvironment(t)
	config, err := loadLLMConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.MaxAttempts != 3 || config.AttemptTimeout != 45*time.Second || config.MaxWallClock != 2*time.Minute {
		t.Fatalf("unexpected retry defaults: %#v", config)
	}
	if config.InitialBackoff != 500*time.Millisecond || config.MaxBackoff != 8*time.Second || config.BackoffJitter != 0.2 {
		t.Fatalf("unexpected backoff defaults: %#v", config)
	}
	if config.MaxInputTokens != 50000 || config.MaxOutputTokens != 8000 || config.MaxTotalTokens != 60000 {
		t.Fatalf("unexpected budget defaults: %#v", config)
	}
}

func TestLoadLLMPolicyOverrides(t *testing.T) {
	clearLLMPolicyEnvironment(t)
	t.Setenv("ORKODA_LLM_PROVIDER", "openrouter")
	t.Setenv("ORKODA_LLM_BASE_URL", "https://example.test/v1")
	t.Setenv("ORKODA_LLM_API_KEY", "secret")
	t.Setenv("ORKODA_LLM_MODEL", "primary-model")
	t.Setenv("ORKODA_LLM_ATTEMPT_TIMEOUT", "30s")
	t.Setenv("ORKODA_LLM_MAX_WALL_CLOCK", "90s")
	t.Setenv("ORKODA_LLM_MAX_ATTEMPTS", "4")
	t.Setenv("ORKODA_LLM_BACKOFF_INITIAL", "250ms")
	t.Setenv("ORKODA_LLM_BACKOFF_MAX", "4s")
	t.Setenv("ORKODA_LLM_BACKOFF_JITTER", "0.1")
	t.Setenv("ORKODA_LLM_MAX_INPUT_TOKENS", "1000")
	t.Setenv("ORKODA_LLM_MAX_OUTPUT_TOKENS", "200")
	t.Setenv("ORKODA_LLM_MAX_TOTAL_TOKENS", "1400")
	t.Setenv("ORKODA_LLM_FALLBACKS_JSON", `[{"provider":"local-fake","model":"local-fake-planner-v1"}]`)
	config, err := loadLLMConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.MaxAttempts != 4 || config.AttemptTimeout != 30*time.Second || config.MaxWallClock != 90*time.Second {
		t.Fatalf("unexpected retry overrides: %#v", config)
	}
	if len(config.Fallbacks) != 1 || config.Fallbacks[0].Provider != "local-fake" {
		t.Fatalf("unexpected fallback config: %#v", config.Fallbacks)
	}
}

func TestLoadLLMPolicyRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "attempts", key: "ORKODA_LLM_MAX_ATTEMPTS", value: "0"},
		{name: "jitter", key: "ORKODA_LLM_BACKOFF_JITTER", value: "1.1"},
		{name: "budget", key: "ORKODA_LLM_MAX_TOTAL_TOKENS", value: "-1"},
		{name: "fallback JSON", key: "ORKODA_LLM_FALLBACKS_JSON", value: "{"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearLLMPolicyEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := loadLLMConfig(); err == nil {
				t.Fatalf("expected %s validation error", test.key)
			}
		})
	}

	clearLLMPolicyEnvironment(t)
	t.Setenv("ORKODA_LLM_BACKOFF_INITIAL", "5s")
	t.Setenv("ORKODA_LLM_BACKOFF_MAX", "1s")
	if _, err := loadLLMConfig(); err == nil {
		t.Fatal("expected invalid backoff range")
	}

	clearLLMPolicyEnvironment(t)
	t.Setenv("ORKODA_LLM_FALLBACKS_JSON", `[{"provider":"local-fake","model":"other"}]`)
	if _, err := loadLLMConfig(); err == nil {
		t.Fatal("expected primary/fallback conflict")
	}
}
