package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/llm"
)

func newTestServer(handler http.Handler) *httptest.Server {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	server.Start()
	return server
}

func TestProviderCompletesStructuredRequest(t *testing.T) {
	var received chatRequest
	server := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer secret-token" {
			t.Fatalf("unexpected authorization header %q", authorization)
		}
		if value := request.Header.Get("X-Title"); value != "Orkoda" {
			t.Fatalf("unexpected custom header %q", value)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"id":"response-1",
			"model":"provider-model",
			"choices":[{
				"message":{"content":"{\"summary\":\"done\"}"},
				"finish_reason":"stop"
			}],
			"usage":{
				"prompt_tokens":12,
				"completion_tokens":5,
				"total_tokens":17,
				"prompt_tokens_details":{"cached_tokens":3}
			}
		}`))
	}))
	defer server.Close()

	provider, err := New(Config{
		Name:         "OpenRouter",
		BaseURL:      server.URL + "/v1",
		APIKey:       "secret-token",
		DefaultModel: "default-model",
		Headers:      map[string]string{"X-Title": "Orkoda"},
		JSONMode:     JSONModeSchema,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Complete(context.Background(), llm.Request{
		Model: "override-model",
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "Be precise"},
			{Role: llm.RoleUser, Content: "Plan this"},
		},
		ResponseSchema:  json.RawMessage(`{"type":"object"}`),
		MaxOutputTokens: 512,
		Temperature:     0.2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if received.Model != "override-model" || received.MaxTokens != 512 {
		t.Fatalf("unexpected request %#v", received)
	}
	if received.ResponseFormat == nil || received.ResponseFormat.Type != "json_schema" || received.ResponseFormat.JSONSchema == nil {
		t.Fatalf("expected JSON schema response format, got %#v", received.ResponseFormat)
	}
	if response.ID != "response-1" || response.Model != "provider-model" || response.FinishReason != llm.FinishReasonStop {
		t.Fatalf("unexpected response %#v", response)
	}
	if response.Usage.InputTokens != 12 || response.Usage.OutputTokens != 5 || response.Usage.CachedInputTokens != 3 {
		t.Fatalf("unexpected usage %#v", response.Usage)
	}
}

func TestProviderUsesDefaultModelAndJSONObjects(t *testing.T) {
	var received chatRequest
	server := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"{}"},"finish_reason":"length"}]}`))
	}))
	defer server.Close()

	provider, err := New(Config{
		Name:         "custom",
		BaseURL:      server.URL,
		APIKey:       "key",
		DefaultModel: "fallback-model",
		JSONMode:     JSONModeObject,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Complete(context.Background(), llm.Request{
		Messages:       []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		ResponseSchema: json.RawMessage(`{"type":"object"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if received.Model != "fallback-model" || received.ResponseFormat == nil || received.ResponseFormat.Type != "json_object" {
		t.Fatalf("unexpected request %#v", received)
	}
	if response.Model != "fallback-model" || response.FinishReason != llm.FinishReasonLength {
		t.Fatalf("unexpected response %#v", response)
	}
}

func TestProviderSupportsArrayContent(t *testing.T) {
	server := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":[{"type":"text","text":"first"},{"type":"output_text","text":" second"}]},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	provider := newTestProvider(t, server.URL)
	response, err := provider.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "first second" {
		t.Fatalf("unexpected content %q", response.Content)
	}
}

func TestProviderNormalizesHTTPErrorsWithoutLeakingBodyOrSecret(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		code       llm.ErrorCode
		retryable  bool
		retryAfter time.Duration
	}{
		{name: "authentication", status: http.StatusUnauthorized, body: `{"error":{"message":"secret-token invalid"}}`, code: llm.ErrorAuthentication},
		{name: "rate limit", status: http.StatusTooManyRequests, body: `{"error":{"message":"slow down"}}`, code: llm.ErrorRateLimited, retryable: true, retryAfter: 2 * time.Second},
		{name: "context", status: http.StatusBadRequest, body: `{"error":{"code":"context_length_exceeded","message":"maximum context length"}}`, code: llm.ErrorContextLength},
		{name: "unavailable", status: http.StatusBadGateway, body: `gateway leaked raw body`, code: llm.ErrorUnavailable, retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if test.retryAfter > 0 {
					writer.Header().Set("Retry-After", "2")
				}
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			provider := newTestProvider(t, server.URL)
			_, err := provider.Complete(context.Background(), testRequest())
			providerError, ok := llm.AsProviderError(err)
			if !ok || providerError.Code != test.code || providerError.Retryable != test.retryable {
				t.Fatalf("unexpected provider error %#v, %v", providerError, err)
			}
			if test.retryAfter > 0 && providerError.RetryAfter != test.retryAfter {
				t.Fatalf("unexpected retry delay %s", providerError.RetryAfter)
			}
			if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "leaked raw body") {
				t.Fatalf("provider error leaked sensitive response: %v", err)
			}
		})
	}
}

func TestProviderRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{`},
		{name: "no choices", body: `{"choices":[]}`},
		{name: "empty content", body: `{"choices":[{"message":{"content":""},"finish_reason":"stop"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			provider := newTestProvider(t, server.URL)
			_, err := provider.Complete(context.Background(), testRequest())
			providerError, ok := llm.AsProviderError(err)
			if !ok || providerError.Code != llm.ErrorInvalidResponse {
				t.Fatalf("expected invalid response error, got %v", err)
			}
		})
	}
}

func TestProviderHonorsCancellationAndTimeout(t *testing.T) {
	server := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
		case <-time.After(250 * time.Millisecond):
			writer.WriteHeader(http.StatusGatewayTimeout)
		}
	}))
	defer server.Close()
	provider, err := New(Config{
		Name:         "custom",
		BaseURL:      server.URL,
		APIKey:       "key",
		DefaultModel: "model",
		HTTPClient:   &http.Client{Timeout: 25 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Complete(context.Background(), testRequest())
	providerError, ok := llm.AsProviderError(err)
	if !ok || providerError.Code != llm.ErrorTimeout {
		t.Fatalf("expected timeout error, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = provider.Complete(ctx, testRequest())
	providerError, ok = llm.AsProviderError(err)
	if !ok || providerError.Code != llm.ErrorCancelled {
		t.Fatalf("expected cancellation error, got %v", err)
	}
}

func TestProviderLimitsResponseSize(t *testing.T) {
	server := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(strings.Repeat("x", 65)))
	}))
	defer server.Close()
	provider, err := New(Config{
		Name:             "custom",
		BaseURL:          server.URL,
		APIKey:           "key",
		DefaultModel:     "model",
		MaxResponseBytes: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Complete(context.Background(), testRequest())
	providerError, ok := llm.AsProviderError(err)
	if !ok || providerError.Code != llm.ErrorInvalidResponse {
		t.Fatalf("expected response size error, got %v", err)
	}
}

func TestProviderBlocksCrossOriginRedirect(t *testing.T) {
	var targetRequests atomic.Int32
	target := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		targetRequests.Add(1)
		if request.Header.Get("Authorization") != "" {
			t.Errorf("authorization leaked to redirect target")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := newTestServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", target.URL)
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	provider := newTestProvider(t, source.URL)
	_, err := provider.Complete(context.Background(), testRequest())
	providerError, ok := llm.AsProviderError(err)
	if !ok || providerError.Code != llm.ErrorUnavailable {
		t.Fatalf("expected blocked redirect error, got %v", err)
	}
	if targetRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests", targetRequests.Load())
	}
}

func TestProviderValidatesConfiguration(t *testing.T) {
	for _, config := range []Config{
		{Name: "", BaseURL: "https://example.com", APIKey: "key", DefaultModel: "model"},
		{Name: "custom", BaseURL: "http://example.com", APIKey: "key", DefaultModel: "model"},
		{Name: "custom", BaseURL: "https://user@example.com", APIKey: "key", DefaultModel: "model"},
		{Name: "custom", BaseURL: "https://example.com", APIKey: "", DefaultModel: "model"},
		{Name: "custom", BaseURL: "https://example.com", APIKey: "key", DefaultModel: ""},
		{Name: "custom", BaseURL: "https://example.com", APIKey: "key", DefaultModel: "model", Headers: map[string]string{"Authorization": "bad"}},
	} {
		if _, err := New(config); err == nil {
			t.Fatalf("expected invalid config %#v", config)
		}
	}
	if _, err := New(Config{Name: "local", BaseURL: "http://127.0.0.1:11434/v1", APIKey: "local", DefaultModel: "model"}); err != nil {
		t.Fatalf("expected loopback HTTP to be allowed: %v", err)
	}
}

func newTestProvider(t *testing.T, baseURL string) *Provider {
	t.Helper()
	provider, err := New(Config{
		Name:         "custom",
		BaseURL:      baseURL,
		APIKey:       "secret-token",
		DefaultModel: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func testRequest() llm.Request {
	return llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	}
}

func TestParseRetryAfterDate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	delay := parseRetryAfter(now.Add(3*time.Second).Format(http.TimeFormat), now)
	if delay != 3*time.Second {
		t.Fatalf("unexpected retry delay %s", delay)
	}
	if !errors.Is(normalizeTransportError("custom", context.Canceled), context.Canceled) {
		t.Fatal("expected provider error to unwrap cancellation")
	}
}
