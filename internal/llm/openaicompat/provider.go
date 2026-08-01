package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/llm"
)

const (
	defaultTimeout      = 60 * time.Second
	defaultResponseSize = int64(4 << 20)
)

type JSONMode string

const (
	JSONModeSchema JSONMode = "json_schema"
	JSONModeObject JSONMode = "json_object"
	JSONModePrompt JSONMode = "prompt_only"
)

type Config struct {
	Name             string
	BaseURL          string
	APIKey           string
	DefaultModel     string
	Headers          map[string]string
	Timeout          time.Duration
	JSONMode         JSONMode
	MaxResponseBytes int64
	HTTPClient       *http.Client
}

type Provider struct {
	name             string
	endpoint         *url.URL
	apiKey           string
	defaultModel     string
	headers          map[string]string
	jsonMode         JSONMode
	maxResponseBytes int64
	client           *http.Client
}

func New(config Config) (*Provider, error) {
	name := strings.ToLower(strings.TrimSpace(config.Name))
	if name == "" {
		return nil, fmt.Errorf("OpenAI-compatible provider name is required")
	}
	baseURL, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse OpenAI-compatible base URL: %w", err)
	}
	if err := validateBaseURL(baseURL); err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI-compatible API key is required")
	}
	defaultModel := strings.TrimSpace(config.DefaultModel)
	if defaultModel == "" {
		return nil, fmt.Errorf("OpenAI-compatible default model is required")
	}
	jsonMode := config.JSONMode
	if jsonMode == "" {
		jsonMode = JSONModeSchema
	}
	if jsonMode != JSONModeSchema && jsonMode != JSONModeObject && jsonMode != JSONModePrompt {
		return nil, fmt.Errorf("unsupported OpenAI-compatible JSON mode %q", jsonMode)
	}

	headers := make(map[string]string, len(config.Headers))
	for key, value := range config.Headers {
		key = http.CanonicalHeaderKey(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		switch strings.ToLower(key) {
		case "authorization", "content-length", "host":
			return nil, fmt.Errorf("custom header %s is not allowed", key)
		}
		headers[key] = value
	}

	endpoint := *baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/chat/completions"
	endpoint.RawQuery = ""
	endpoint.Fragment = ""

	client := config.HTTPClient
	if client == nil {
		timeout := config.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		client = &http.Client{Timeout: timeout}
	}
	originalRedirect := client.CheckRedirect
	clientCopy := *client
	clientCopy.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		if len(via) > 0 && !sameOrigin(via[0].URL, request.URL) {
			return errors.New("redirect to a different origin is not allowed")
		}
		if originalRedirect != nil {
			return originalRedirect(request, via)
		}
		return nil
	}

	maxResponseBytes := config.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultResponseSize
	}
	return &Provider{
		name:             name,
		endpoint:         &endpoint,
		apiKey:           apiKey,
		defaultModel:     defaultModel,
		headers:          headers,
		jsonMode:         jsonMode,
		maxResponseBytes: maxResponseBytes,
		client:           &clientCopy,
	}, nil
}

func (p *Provider) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

func (p *Provider) Info() llm.ProviderInfo {
	if p == nil {
		return llm.ProviderInfo{}
	}
	return llm.ProviderInfo{
		Name:             p.name,
		DefaultModel:     p.defaultModel,
		Configured:       true,
		StructuredOutput: p.jsonMode != JSONModePrompt,
	}
}

func (p *Provider) Complete(ctx context.Context, request llm.Request) (llm.Response, error) {
	if p == nil {
		return llm.Response{}, fmt.Errorf("OpenAI-compatible provider is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, model, err := p.buildRequest(request)
	if err != nil {
		return llm.Response{}, providerError(p.name, llm.ErrorInvalidRequest, "invalid OpenAI-compatible request", false, 0, err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return llm.Response{}, providerError(p.name, llm.ErrorInvalidRequest, "encode OpenAI-compatible request", false, 0, err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return llm.Response{}, providerError(p.name, llm.ErrorInvalidRequest, "create OpenAI-compatible request", false, 0, err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	for key, value := range p.headers {
		httpRequest.Header.Set(key, value)
	}

	httpResponse, err := p.client.Do(httpRequest)
	if err != nil {
		return llm.Response{}, normalizeTransportError(p.name, err)
	}
	defer httpResponse.Body.Close()
	responseBody, err := readLimited(httpResponse.Body, p.maxResponseBytes)
	if err != nil {
		return llm.Response{}, providerError(p.name, llm.ErrorInvalidResponse, "OpenAI-compatible response exceeded the size limit", false, 0, err)
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		return llm.Response{}, normalizeHTTPError(p.name, httpResponse, responseBody)
	}

	var decoded chatResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return llm.Response{}, providerError(p.name, llm.ErrorInvalidResponse, "OpenAI-compatible provider returned invalid JSON", false, 0, err)
	}
	if len(decoded.Choices) == 0 {
		return llm.Response{}, providerError(p.name, llm.ErrorInvalidResponse, "OpenAI-compatible provider returned no choices", false, 0, nil)
	}
	content, err := decoded.Choices[0].Message.text()
	if err != nil || strings.TrimSpace(content) == "" {
		return llm.Response{}, providerError(p.name, llm.ErrorInvalidResponse, "OpenAI-compatible provider returned empty content", false, 0, err)
	}
	responseModel := strings.TrimSpace(decoded.Model)
	if responseModel == "" {
		responseModel = model
	}
	return llm.Response{
		ID:           strings.TrimSpace(decoded.ID),
		Model:        responseModel,
		Content:      content,
		FinishReason: mapFinishReason(decoded.Choices[0].FinishReason),
		Usage: llm.Usage{
			InputTokens:       decoded.Usage.PromptTokens,
			OutputTokens:      decoded.Usage.CompletionTokens,
			CachedInputTokens: decoded.Usage.PromptTokensDetails.CachedTokens,
			TotalTokens:       decoded.Usage.TotalTokens,
		},
	}, nil
}

func (p *Provider) buildRequest(request llm.Request) (chatRequest, string, error) {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = p.defaultModel
	}
	if model == "" {
		return chatRequest{}, "", errors.New("model is required")
	}
	messages := make([]chatMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		role := string(message.Role)
		if role != string(llm.RoleSystem) && role != string(llm.RoleUser) && role != string(llm.RoleAssistant) {
			return chatRequest{}, "", fmt.Errorf("unsupported role %q", role)
		}
		messages = append(messages, chatMessage{Role: role, Content: message.Content})
	}
	if len(messages) == 0 {
		return chatRequest{}, "", errors.New("at least one message is required")
	}
	payload := chatRequest{
		Model:       model,
		Messages:    messages,
		Temperature: request.Temperature,
	}
	if request.MaxOutputTokens > 0 {
		payload.MaxTokens = request.MaxOutputTokens
	}
	if len(request.ResponseSchema) > 0 {
		switch p.jsonMode {
		case JSONModeSchema:
			var schema any
			if err := json.Unmarshal(request.ResponseSchema, &schema); err != nil {
				return chatRequest{}, "", fmt.Errorf("decode response schema: %w", err)
			}
			payload.ResponseFormat = &responseFormat{
				Type: "json_schema",
				JSONSchema: &jsonSchema{
					Name:   "orkoda_planning_response",
					Strict: true,
					Schema: schema,
				},
			}
		case JSONModeObject:
			payload.ResponseFormat = &responseFormat{Type: "json_object"}
		case JSONModePrompt:
		}
	}
	return payload, model, nil
}

func validateBaseURL(value *url.URL) error {
	if value == nil || value.Scheme == "" || value.Host == "" {
		return fmt.Errorf("OpenAI-compatible base URL must be absolute")
	}
	if value.User != nil {
		return fmt.Errorf("OpenAI-compatible base URL must not contain credentials")
	}
	if value.Scheme == "https" {
		return nil
	}
	if value.Scheme == "http" && isLoopbackHost(value.Hostname()) {
		return nil
	}
	return fmt.Errorf("OpenAI-compatible base URL must use HTTPS unless it targets loopback")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func sameOrigin(left, right *url.URL) bool {
	return left != nil && right != nil && strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	content, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, errors.New("response body is too large")
	}
	return content, nil
}

func normalizeTransportError(provider string, err error) error {
	if errors.Is(err, context.Canceled) {
		return providerError(provider, llm.ErrorCancelled, "OpenAI-compatible request was cancelled", false, 0, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return providerError(provider, llm.ErrorTimeout, "OpenAI-compatible request timed out", true, 0, err)
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return providerError(provider, llm.ErrorTimeout, "OpenAI-compatible request timed out", true, 0, err)
	}
	return providerError(provider, llm.ErrorUnavailable, "OpenAI-compatible provider is unavailable", true, 0, err)
}

func normalizeHTTPError(provider string, response *http.Response, body []byte) error {
	code := llm.ErrorUnknown
	message := "OpenAI-compatible provider request failed"
	retryable := false
	retryAfter := time.Duration(0)
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		code = llm.ErrorAuthentication
		message = "OpenAI-compatible provider authentication failed"
	case http.StatusRequestTimeout:
		code = llm.ErrorTimeout
		message = "OpenAI-compatible provider request timed out"
		retryable = true
	case http.StatusTooManyRequests:
		code = llm.ErrorRateLimited
		message = "OpenAI-compatible provider rate limit exceeded"
		retryable = true
		retryAfter = parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	case http.StatusBadRequest:
		if responseIndicatesContextLength(body) {
			code = llm.ErrorContextLength
			message = "OpenAI-compatible provider context length was exceeded"
		} else {
			code = llm.ErrorInvalidRequest
			message = "OpenAI-compatible provider rejected the request"
		}
	default:
		if response.StatusCode >= 500 {
			code = llm.ErrorUnavailable
			message = "OpenAI-compatible provider is unavailable"
			retryable = true
		}
	}
	return providerError(provider, code, message, retryable, retryAfter, nil)
}

func responseIndicatesContextLength(body []byte) bool {
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	value := strings.ToLower(payload.Error.Code + " " + payload.Error.Type + " " + payload.Error.Message)
	return strings.Contains(value, "context_length") || strings.Contains(value, "context length") || strings.Contains(value, "maximum context")
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if timestamp, err := http.ParseTime(value); err == nil && timestamp.After(now) {
		return timestamp.Sub(now)
	}
	return 0
}

func providerError(provider string, code llm.ErrorCode, message string, retryable bool, retryAfter time.Duration, cause error) error {
	return &llm.ProviderError{
		Provider:   provider,
		Code:       code,
		Message:    message,
		Retryable:  retryable,
		RetryAfter: retryAfter,
		Cause:      cause,
	}
}

func mapFinishReason(value string) llm.FinishReason {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "stop":
		return llm.FinishReasonStop
	case "length", "max_tokens":
		return llm.FinishReasonLength
	case "tool_calls", "function_call":
		return llm.FinishReasonToolCall
	case "content_filter":
		return llm.FinishReasonContentFilter
	default:
		return llm.FinishReasonUnknown
	}
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Temperature    float64         `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *jsonSchema `json:"json_schema,omitempty"`
}

type jsonSchema struct {
	Name   string `json:"name"`
	Strict bool   `json:"strict"`
	Schema any    `json:"schema"`
}

type chatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      responseMessage `json:"message"`
		FinishReason string          `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

type responseMessage struct {
	Content json.RawMessage `json:"content"`
}

func (m responseMessage) text() (string, error) {
	if len(m.Content) == 0 || string(m.Content) == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(m.Content, &text); err == nil {
		return text, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return "", err
	}
	var builder strings.Builder
	for _, part := range parts {
		if part.Type == "text" || part.Type == "output_text" || part.Type == "" {
			builder.WriteString(part.Text)
		}
	}
	return builder.String(), nil
}
