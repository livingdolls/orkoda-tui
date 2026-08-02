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
	defaultLLMProvider                      = "local-fake"
	defaultLLMTimeout                       = 60 * time.Second
	defaultLLMJSONMode                      = "json_schema"
	defaultAttemptTimeout                   = 45 * time.Second
	defaultMaxWallClock                     = 2 * time.Minute
	defaultMaxAttempts                      = 3
	defaultInitialBackoff                   = 500 * time.Millisecond
	defaultMaxBackoff                       = 8 * time.Second
	defaultBackoffJitter                    = 0.2
	defaultMaxInputTokens                   = 50000
	defaultMaxOutputTokens                  = 8000
	defaultMaxTotalTokens                   = 60000
	defaultLLMRedactionMode                 = "strict"
	defaultLLMMaxRepairAttempts             = 1
	defaultLLMMaxStructuredResponseBytes    = 1 << 20
)

type LLMFallbackConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
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
	config := LLMConfig{
		Provider:                   strings.ToLower(strings.TrimSpace(stringFromEnv("ORKODA_LLM_PROVIDER", defaultLLMProvider))),
		BaseURL:                    strings.TrimSpace(os.Getenv("ORKODA_LLM_BASE_URL")),
		APIKey:                     strings.TrimSpace(os.Getenv("ORKODA_LLM_API_KEY")),
		Model:                      strings.TrimSpace(os.Getenv("ORKODA_LLM_MODEL")),
		JSONMode:                   strings.ToLower(strings.TrimSpace(stringFromEnv("ORKODA_LLM_JSON_MODE", defaultLLMJSONMode))),
		Timeout:                    timeout,
		Headers:                    headers,
		AttemptTimeout:             attemptTimeout,
		MaxWallClock:               maxWallClock,
		MaxAttempts:                maxAttempts,
		InitialBackoff:             initialBackoff,
		MaxBackoff:                 maxBackoff,
		BackoffJitter:              backoffJitter,
		MaxInputTokens:             maxInputTokens,
		MaxOutputTokens:            maxOutputTokens,
		MaxTotalTokens:             maxTotalTokens,
		Fallbacks:                  fallbacks,
		RedactionMode:              strings.ToLower(strings.TrimSpace(stringFromEnv("ORKODA_LLM_REDACTION_MODE", defaultLLMRedactionMode))),
		MaxRepairAttempts:          maxRepairAttempts,
		MaxStructuredResponseBytes: maxStructuredResponseBytes,
	}
	if config.Provider == "" {
		config.Provider = defaultLLMProvider
	}
	if config.Provider != defaultLLMProvider {
		if config.BaseURL == "" {
			return LLMConfig{}, fmt.Errorf("ORKODA_LLM_BASE_URL is required when ORKODA_LLM_PROVIDER is %s", config.Provider)
		}
		if config.APIKey == "" {
			return LLMConfig{}, fmt.Errorf("ORKODA_LLM_API_KEY is required when ORKODA_LLM_PROVIDER is %s", config.Provider)
		}
		if config.Model == "" {
			return LLMConfig{}, fmt.Errorf("ORKODA_LLM_MODEL is required when ORKODA_LLM_PROVIDER is %s", config.Provider)
		}
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
		key := fallback.Provider + "\x00" + fallback.Model
		if _, exists := seenFallbacks[key]; exists {
			return LLMConfig{}, fmt.Errorf("duplicate LLM fallback target %s/%s", fallback.Provider, fallback.Model)
		}
		seenFallbacks[key] = struct{}{}
	}
	return config, nil
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
