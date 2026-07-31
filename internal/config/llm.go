package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultLLMProvider = "local-fake"
	defaultLLMTimeout  = 60 * time.Second
	defaultLLMJSONMode = "json_schema"
)

type LLMConfig struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
	JSONMode string
	Timeout  time.Duration
	Headers  map[string]string
}

func loadLLMConfig() (LLMConfig, error) {
	timeout, err := durationFromEnv("ORKODA_LLM_TIMEOUT", defaultLLMTimeout)
	if err != nil {
		return LLMConfig{}, err
	}
	headers, err := stringMapFromEnv("ORKODA_LLM_HEADERS_JSON")
	if err != nil {
		return LLMConfig{}, err
	}
	config := LLMConfig{
		Provider: strings.ToLower(strings.TrimSpace(stringFromEnv("ORKODA_LLM_PROVIDER", defaultLLMProvider))),
		BaseURL:  strings.TrimSpace(os.Getenv("ORKODA_LLM_BASE_URL")),
		APIKey:   strings.TrimSpace(os.Getenv("ORKODA_LLM_API_KEY")),
		Model:    strings.TrimSpace(os.Getenv("ORKODA_LLM_MODEL")),
		JSONMode: strings.ToLower(strings.TrimSpace(stringFromEnv("ORKODA_LLM_JSON_MODE", defaultLLMJSONMode))),
		Timeout:  timeout,
		Headers:  headers,
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
