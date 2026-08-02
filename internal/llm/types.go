package llm

import (
	"context"
	"encoding/json"
	"strings"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type FinishReason string

const (
	FinishReasonStop          FinishReason = "STOP"
	FinishReasonLength        FinishReason = "LENGTH"
	FinishReasonToolCall      FinishReason = "TOOL_CALL"
	FinishReasonContentFilter FinishReason = "CONTENT_FILTER"
	FinishReasonCancelled     FinishReason = "CANCELLED"
	FinishReasonUnknown       FinishReason = "UNKNOWN"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model           string            `json:"model"`
	Messages        []Message         `json:"messages"`
	ResponseSchema  json.RawMessage   `json:"response_schema,omitempty"`
	MaxOutputTokens int               `json:"max_output_tokens,omitempty"`
	Temperature     float64           `json:"temperature,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type Usage struct {
	InputTokens          int    `json:"input_tokens"`
	OutputTokens         int    `json:"output_tokens"`
	CachedInputTokens    int    `json:"cached_input_tokens,omitempty"`
	TotalTokens          int    `json:"total_tokens"`
	AttemptCount         int    `json:"attempt_count,omitempty"`
	FallbackUsed         bool   `json:"fallback_used,omitempty"`
	FinalProvider        string `json:"final_provider,omitempty"`
	FinalModel           string `json:"final_model,omitempty"`
	EstimatedInputTokens int    `json:"estimated_input_tokens,omitempty"`
	EstimatedTokensSpent int    `json:"estimated_tokens_spent,omitempty"`
}

type Response struct {
	ID           string            `json:"id"`
	Model        string            `json:"model"`
	Content      string            `json:"content"`
	FinishReason FinishReason      `json:"finish_reason"`
	Usage        Usage             `json:"usage"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Provider interface {
	Name() string
	Complete(context.Context, Request) (Response, error)
}

type Gateway interface {
	Complete(context.Context, string, Request) (Response, error)
}

func cloneRequest(request Request) Request {
	cloned := request
	cloned.Messages = append([]Message(nil), request.Messages...)
	cloned.ResponseSchema = append(json.RawMessage(nil), request.ResponseSchema...)
	cloned.Metadata = cloneStrings(request.Metadata)
	return cloned
}

func cloneResponse(response Response) Response {
	cloned := response
	cloned.Metadata = cloneStrings(response.Metadata)
	return cloned
}

func cloneStrings(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func normalizeUsage(usage Usage) Usage {
	if usage.InputTokens < 0 {
		usage.InputTokens = 0
	}
	if usage.OutputTokens < 0 {
		usage.OutputTokens = 0
	}
	if usage.CachedInputTokens < 0 {
		usage.CachedInputTokens = 0
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	return usage
}

func normalizeResponse(request Request, response Response) Response {
	response.ID = strings.TrimSpace(response.ID)
	response.Model = strings.TrimSpace(response.Model)
	if response.Model == "" {
		response.Model = strings.TrimSpace(request.Model)
	}
	if response.FinishReason == "" {
		response.FinishReason = FinishReasonUnknown
	}
	response.Usage = normalizeUsage(response.Usage)
	response.Metadata = cloneStrings(response.Metadata)
	return response
}
