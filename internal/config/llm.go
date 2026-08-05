package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLLMProvider                   = "local-fake"
	defaultLLMTimeout                    = 60 * time.Second
	defaultLLMJSONMode                   = "json_schema"
	defaultAttemptTimeout                = 45 * time.Second
	defaultMaxWallClock                  = 2 * time.Minute
	defaultMaxAttempts                   = 3
	defaultInitialBackoff                = 500 * time.Millisecond
	defaultMaxBackoff                    = 8 * time.Second
	defaultBackoffJitter                 = 0.2
	defaultMaxInputTokens                = 50000
	defaultMaxOutputTokens               = 8000
	defaultMaxTotalTokens                = 60000
	defaultLLMRedactionMode              = "strict"
	defaultLLMMaxRepairAttempts          = 1
	defaultLLMMaxStructuredResponseBytes = 1 << 20
)

type LLMFallbackConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type LLMProviderConfig struct {
	Name     string
	BaseURL  string
	APIKey   string
	Model    string
	JSONMode string
	Timeout  time.Duration
	Headers  map[string]string
}

type rawLLMProviderConfig struct {
	Name      string            `json:"name"`
	BaseURL   string            `json:"base_url"`
	APIKey    string            `json:"api_key,omitempty"`
	APIKeyEnv string            `json:"api_key_env,omitempty"`
	Model     string            `json:"model"`
	JSONMode  string            `json:"json_mode,omitempty"`
	Timeout   string            `json:"timeout,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type LLMConfig struct {
	Provider                   string
	BaseURL                    string
	APIKey                     string
	Model                      string
	JSONMode                   string
	Timeout                    time.Duration
	Headers                    map[string]string
	AttemptTimeout             time.Duration
	MaxWallClock               time.Duration
	MaxAttempts                int
	InitialBackoff             time.Duration
	MaxBackoff                 time.Duration
	BackoffJitter              float64
	MaxInputTokens             int
	MaxOutputTokens            int
	MaxTotalTokens             int
	Fallbacks                  []LLMFallbackConfig
	Providers                  []LLMProviderConfig
	RedactionMode              string
	MaxRepairAttempts          int
	MaxStructuredResponseBytes int
}

func loadLLMConfig() (LLMConfig, error) {
	timeout, err := durationFromEnv("ORKODA_LLM_TIMEOUT", defaultLLMTimeout)
	if err != nil {
		return LLMConfig{}, err
	}
	attemptTimeout, err := durationFromEnv("ORKODA_LLM_ATTEMPT_TIMEOUT", defaultAttemptTimeout)
	if err != nil {
		return LLMConfig{}, err
	}
	maxWallClock, err := durationFromEnv("ORKODA_LLM_MAX_WALL_CLOCK", defaultMaxWallClock)
	if err != nil {
		return LLMConfig{}, err
	}
	initialBackoff, err := durationFromEnv("ORKODA_LLM_BACKOFF_INITIAL", defaultInitialBackoff)
	if err != nil {
		return LLMConfig{}, err
	}
	maxBackoff, err := durationFromEnv("ORKODA_LLM_BACKOFF_MAX", defaultMaxBackoff)
	if err != nil {
		return LLMConfig{}, err
	}
	maxAttempts, err := positiveIntFromEnv("ORKODA_LLM_MAX_ATTEMPTS", defaultMaxAttempts)
	if err != nil {
		return LLMConfig{}, err
	}
	backoffJitter, err := fractionFromEnv("ORKODA_LLM_BACKOFF_JITTER", defaultBackoffJitter)
	if err != nil {
		return LLMConfig{}, err
	}
	maxInputTokens, err := nonNegativeIntFromEnv("ORKODA_LLM_MAX_INPUT_TOKENS", defaultMaxInputTokens)
	if err != nil {
		return LLMConfig{}, err
	}
	maxOutputTokens, err := nonNegativeIntFromEnv("ORKODA_LLM_MAX_OUTPUT_TOKENS", defaultMaxOutputTokens)
	if err != nil {
		return LLMConfig{}, err
	}
	maxTotalTokens, err := nonNegativeIntFromEnv("ORKODA_LLM_MAX_TOTAL_TOKENS", defaultMaxTotalTokens)
	if err != nil {
		return LLMConfig{}, err
	}
	maxRepairAttempts, err := nonNegativeIntFromEnv("ORKODA_LLM_MAX_REPAIR_ATTEMPTS", defaultLLMMaxRepairAttempts)
	if err != nil {
		return LLMConfig{}, err
	}
	maxStructuredResponseBytes, err := positiveIntFromEnv(
		"ORKODA_LLM_MAX_STRUCTURED_RESPONSE_BYTES",
		defaultLLMMaxStructuredResponseBytes,
	)
	if err != nil {
		return LLMConfig{}, err
	}
	headers, err := stringMapFromEnv("ORKODA_LLM_HEADERS_JSON")
	if err != nil {
		return LLMConfig{}, err
	}
	fallbacks, err := fallbackConfigFromEnv("ORKODA_LLM_FALLBACKS_JSON")
	if err != nil {
		return LLMConfig{}, err
	}
	jsonMode := strings.ToLower(strings.TrimSpace(stringFromEnv("ORKODA_LLM_JSON_MODE", defaultLLMJSONMode)))
	providers, err := providerConfigsFromEnv("ORKODA_LLM_PROVIDERS_JSON", timeout, jsonMode, headers)
	if err != nil {
		return LLMConfig{}, err
	}

	configuredDefault := strings.ToLower(strings.TrimSpace(os.Getenv("ORKODA_LLM_PROVIDER")))
	providerName := configuredDefault
	if providerName == "" {
		if len(providers) > 0 {
			providerName = providers[0].Name
		} else {
			providerName = defaultLLMProvider
		}
	}
	legacyBaseURL := strings.TrimSpace(os.Getenv("ORKODA_LLM_BASE_URL"))
	legacyAPIKey := strings.TrimSpace(os.Getenv("ORKODA_LLM_API_KEY"))
	legacyModel := strings.TrimSpace(os.Getenv("ORKODA_LLM_MODEL"))
	if len(providers) == 0 && providerName != defaultLLMProvider {
		if legacyBaseURL == "" {
			return LLMConfig{}, fmt.Errorf("ORKODA_LLM_BASE_URL is required when ORKODA_LLM_PROVIDER is %s", providerName)
		}
		if legacyAPIKey == "" {
			return LLMConfig{}, fmt.Errorf("ORKODA_LLM_API_KEY is required when ORKODA_LLM_PROVIDER is %s", providerName)
		}
		if legacyModel == "" {
			return LLMConfig{}, fmt.Errorf("ORKODA_LLM_MODEL is required when ORKODA_LLM_PROVIDER is %s", providerName)
		}
		providers = []LLMProviderConfig{{
			Name: providerName, BaseURL: legacyBaseURL, APIKey: legacyAPIKey,
			Model: legacyModel, JSONMode: jsonMode, Timeout: timeout, Headers: headers,
		}}
	}

	defaultModel := legacyModel
	defaultBaseURL := legacyBaseURL
	defaultAPIKey := legacyAPIKey
	registered := map[string]struct{}{defaultLLMProvider: {}}
	for _, provider := range providers {
		registered[provider.Name] = struct{}{}
		if provider.Name == providerName {
			if defaultModel == "" {
				defaultModel = provider.Model
			}
			defaultBaseURL = provider.BaseURL
			defaultAPIKey = provider.APIKey
			jsonMode = provider.JSONMode
			timeout = provider.Timeout
			headers = provider.Headers
		}
	}
	if _, exists := registered[providerName]; !exists {
		return LLMConfig{}, fmt.Errorf("default LLM provider %s is not registered", providerName)
	}
	if providerName != defaultLLMProvider && defaultModel == "" {
		return LLMConfig{}, fmt.Errorf("default LLM provider %s requires a model", providerName)
	}

	config := LLMConfig{
		Provider: providerName, BaseURL: defaultBaseURL, APIKey: defaultAPIKey,
		Model: defaultModel, JSONMode: jsonMode, Timeout: timeout, Headers: headers,
		AttemptTimeout: attemptTimeout, MaxWallClock: maxWallClock, MaxAttempts: maxAttempts,
		InitialBackoff: initialBackoff, MaxBackoff: maxBackoff, BackoffJitter: backoffJitter,
		MaxInputTokens: maxInputTokens, MaxOutputTokens: maxOutputTokens,
		MaxTotalTokens: maxTotalTokens, Fallbacks: fallbacks, Providers: providers,
		RedactionMode:     strings.ToLower(strings.TrimSpace(stringFromEnv("ORKODA_LLM_REDACTION_MODE", defaultLLMRedactionMode))),
		MaxRepairAttempts: maxRepairAttempts, MaxStructuredResponseBytes: maxStructuredResponseBytes,
	}
	if config.InitialBackoff > config.MaxBackoff {
		return LLMConfig{}, fmt.Errorf("ORKODA_LLM_BACKOFF_MAX must not be smaller than ORKODA_LLM_BACKOFF_INITIAL")
	}
	switch config.RedactionMode {
	case "strict", "report", "off":
	default:
		return LLMConfig{}, fmt.Errorf("ORKODA_LLM_REDACTION_MODE must be strict, report, or off")
	}
	seenFallbacks := make(map[string]struct{}, len(config.Fallbacks))
	for _, fallback := range config.Fallbacks {
		if fallback.Provider == config.Provider {
			return LLMConfig{}, fmt.Errorf("fallback provider %s must differ from ORKODA_LLM_PROVIDER", fallback.Provider)
		}
		if _, exists := registered[fallback.Provider]; !exists {
			return LLMConfig{}, fmt.Errorf("fallback provider %s is not registered", fallback.Provider)
		}
		key := fallback.Provider + "\x00" + fallback.Model
		if _, exists := seenFallbacks[key]; exists {
			return LLMConfig{}, fmt.Errorf("duplicate LLM fallback target %s/%s", fallback.Provider, fallback.Model)
		}
		seenFallbacks[key] = struct{}{}
	}
	return config, nil
}

func providerConfigsFromEnv(
	key string,
	defaultTimeout time.Duration,
	defaultJSONMode string,
	defaultHeaders map[string]string,
) ([]LLMProviderConfig, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil, nil
	}
	var raw []rawLLMProviderConfig
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", key, err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("%s must contain at least one provider", key)
	}
	seen := make(map[string]struct{}, len(raw))
	providers := make([]LLMProviderConfig, 0, len(raw))
	for index, item := range raw {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		if name == "" || name == defaultLLMProvider {
			return nil, fmt.Errorf("%s entry %d requires a non-local provider name", key, index)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("%s contains duplicate provider %s", key, name)
		}
		seen[name] = struct{}{}
		baseURL := strings.TrimSpace(item.BaseURL)
		model := strings.TrimSpace(item.Model)
		if baseURL == "" || model == "" {
			return nil, fmt.Errorf("%s provider %s requires base_url and model", key, name)
		}
		apiKey := strings.TrimSpace(item.APIKey)
		apiKeyEnv := strings.TrimSpace(item.APIKeyEnv)
		if apiKey == "" && apiKeyEnv != "" {
			apiKey = strings.TrimSpace(os.Getenv(apiKeyEnv))
		}
		if apiKey == "" {
			return nil, fmt.Errorf("%s provider %s requires api_key or a populated api_key_env", key, name)
		}
		timeout := defaultTimeout
		if strings.TrimSpace(item.Timeout) != "" {
			parsed, err := time.ParseDuration(strings.TrimSpace(item.Timeout))
			if err != nil || parsed <= 0 {
				return nil, fmt.Errorf("%s provider %s has invalid timeout", key, name)
			}
			timeout = parsed
		}
		jsonMode := strings.ToLower(strings.TrimSpace(item.JSONMode))
		if jsonMode == "" {
			jsonMode = defaultJSONMode
		}
		switch jsonMode {
		case "json_schema", "json_object", "prompt_only":
		default:
			return nil, fmt.Errorf("%s provider %s has invalid json_mode", key, name)
		}
		headers := item.Headers
		if headers == nil {
			headers = defaultHeaders
		}
		providers = append(providers, LLMProviderConfig{
			Name: name, BaseURL: baseURL, APIKey: apiKey, Model: model,
			JSONMode: jsonMode, Timeout: timeout, Headers: headers,
		})
	}
	return providers, nil
}

func stringMapFromEnv(key string) (map[string]string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil, nil
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(value), &headers); err != nil {
		return nil, fmt.Errorf("parse %s: %w", key, err)
	}
	for name, headerValue := range headers {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(headerValue) == "" {
			delete(headers, name)
		}
	}
	return headers, nil
}

func fallbackConfigFromEnv(key string) ([]LLMFallbackConfig, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil, nil
	}
	var fallbacks []LLMFallbackConfig
	if err := json.Unmarshal([]byte(value), &fallbacks); err != nil {
		return nil, fmt.Errorf("parse %s: %w", key, err)
	}
	for index := range fallbacks {
		fallbacks[index].Provider = strings.ToLower(strings.TrimSpace(fallbacks[index].Provider))
		fallbacks[index].Model = strings.TrimSpace(fallbacks[index].Model)
		if fallbacks[index].Provider == "" || fallbacks[index].Model == "" {
			return nil, fmt.Errorf("%s entries require provider and model", key)
		}
	}
	return fallbacks, nil
}

func positiveIntFromEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func nonNegativeIntFromEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", key)
	}
	return parsed, nil
}

func fractionFromEnv(key string, fallback float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 || parsed > 1 {
		return 0, fmt.Errorf("%s must be between zero and one", key)
	}
	return parsed, nil
}
