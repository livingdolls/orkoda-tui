package llm

import (
	"context"
	"encoding/json"
	"strconv"
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
	InputTokens          int     `json:"input_tokens"`
	OutputTokens         int     `json:"output_tokens"`
	CachedInputTokens    int     `json:"cached_input_tokens,omitempty"`
	TotalTokens          int     `json:"total_tokens"`
	AttemptCount         int     `json:"attempt_count,omitempty"`
	FallbackUsed         bool    `json:"fallback_used,omitempty"`
	FinalProvider        string  `json:"final_provider,omitempty"`
	FinalModel           string  `json:"final_model,omitempty"`
	EstimatedInputTokens int     `json:"estimated_input_tokens,omitempty"`
	EstimatedTokensSpent int     `json:"estimated_tokens_spent,omitempty"`
	ValidationAttempts   int     `json:"validation_attempts,omitempty"`
	ValidationErrorCount int     `json:"validation_error_count,omitempty"`
	RepairUsed           bool    `json:"repair_used,omitempty"`
	RedactionCount       int     `json:"redaction_count,omitempty"`
	EstimatedCostUSD     float64 `json:"estimated_cost_usd,omitempty"`
}

type Response struct {
	ID           string            `json:"id"`
	Model        string            `json:"model"`
	Content      string            `json:"content"`
	FinishReason FinishReason      `json:"finish_reason"`
	Usage        Usage             `json:"usage"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type ProviderInfo struct {
	Name             string `json:"name"`
	DefaultModel     string `json:"default_model"`
	Configured       bool   `json:"configured"`
	StructuredOutput bool   `json:"structured_output"`
	Default          bool   `json:"default"`
	BaseURL          string `json:"base_url,omitempty"`
	JSONMode         string `json:"json_mode,omitempty"`
	TimeoutMS        int64  `json:"timeout_ms,omitempty"`
	CredentialStored bool   `json:"credential_stored"`
	Source           string `json:"source,omitempty"`
	Editable         bool   `json:"editable"`
	Deletable        bool   `json:"deletable"`
}

type Provider interface {
	Name() string
	Complete(context.Context, Request) (Response, error)
}

type ProviderDescriber interface {
	Info() ProviderInfo
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
	applyExecutionMetadata(&cloned.Usage, cloned.Metadata)
	return cloned
}

func applyExecutionMetadata(usage *Usage, metadata map[string]string) {
	if usage == nil || len(metadata) == 0 {
		return
	}
	usage.FinalProvider = strings.TrimSpace(metadata["final_provider"])
	usage.FinalModel = strings.TrimSpace(metadata["final_model"])
	usage.AttemptCount, _ = strconv.Atoi(metadata["attempt_count"])
	usage.FallbackUsed, _ = strconv.ParseBool(metadata["fallback_used"])
	usage.EstimatedInputTokens, _ = strconv.Atoi(metadata["estimated_input_tokens"])
	usage.EstimatedTokensSpent, _ = strconv.Atoi(metadata["estimated_tokens_spent"])
	usage.ValidationAttempts, _ = strconv.Atoi(metadata["validation_attempts"])
	usage.ValidationErrorCount, _ = strconv.Atoi(metadata["validation_error_count"])
	usage.RepairUsed, _ = strconv.ParseBool(metadata["repair_used"])
	usage.RedactionCount, _ = strconv.Atoi(metadata["redaction_count"])
	usage.EstimatedCostUSD, _ = strconv.ParseFloat(metadata["estimated_cost_usd"], 64)
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
	if usage.EstimatedCostUSD < 0 {
		usage.EstimatedCostUSD = 0
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
