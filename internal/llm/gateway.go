package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type GatewayService struct {
	registry *Registry
	recorder EventRecorder
	now      func() time.Time
}

func NewGateway(registry *Registry, recorder EventRecorder) (*GatewayService, error) {
	if registry == nil {
		return nil, fmt.Errorf("LLM provider registry is required")
	}
	return &GatewayService{
		registry: registry,
		recorder: recorder,
		now:      func() time.Time { return time.Now().UTC() },
	}, nil
}

func (g *GatewayService) Complete(ctx context.Context, providerName string, request Request) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	providerName = normalizeProviderName(providerName)
	provider, err := g.registry.Provider(providerName)
	if err != nil {
		return Response{}, &ProviderError{
			Provider: providerName,
			Code:     ErrorUnavailable,
			Message:  "provider is not registered",
			Cause:    err,
		}
	}
	if err := validateRequest(providerName, request); err != nil {
		return Response{}, err
	}

	request = cloneRequest(request)
	startedAt := g.now()
	g.record(ctx, "llm.request_started", eventPayload(providerName, request, 0))

	response, err := provider.Complete(ctx, request)
	duration := g.now().Sub(startedAt)
	if duration < 0 {
		duration = 0
	}
	if err != nil {
		providerError := normalizeProviderError(providerName, err)
		payload := eventPayload(providerName, request, duration)
		payload["error_code"] = providerError.Code
		payload["retryable"] = providerError.Retryable
		if providerError.RetryAfter > 0 {
			payload["retry_after_ms"] = providerError.RetryAfter.Milliseconds()
		}
		eventType := "llm.request_failed"
		if providerError.Code == ErrorCancelled {
			eventType = "llm.request_cancelled"
		}
		g.record(ctx, eventType, payload)
		return Response{}, providerError
	}

	response = normalizeResponse(request, response)
	payload := eventPayload(providerName, request, duration)
	payload["response_id"] = response.ID
	payload["finish_reason"] = response.FinishReason
	payload["input_tokens"] = response.Usage.InputTokens
	payload["output_tokens"] = response.Usage.OutputTokens
	payload["cached_input_tokens"] = response.Usage.CachedInputTokens
	payload["total_tokens"] = response.Usage.TotalTokens
	g.record(ctx, "llm.request_completed", payload)
	return cloneResponse(response), nil
}

func normalizeProviderError(provider string, err error) *ProviderError {
	if err == nil {
		return nil
	}
	if providerError, ok := AsProviderError(err); ok {
		cloned := *providerError
		if strings.TrimSpace(cloned.Provider) == "" {
			cloned.Provider = provider
		}
		if cloned.Code == "" {
			cloned.Code = ErrorUnknown
		}
		return &cloned
	}
	if errors.Is(err, context.Canceled) {
		return &ProviderError{
			Provider: provider,
			Code:     ErrorCancelled,
			Message:  "request was cancelled",
			Cause:    err,
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &ProviderError{
			Provider:  provider,
			Code:      ErrorTimeout,
			Message:   "request deadline exceeded",
			Retryable: true,
			Cause:     err,
		}
	}
	return &ProviderError{
		Provider: provider,
		Code:     ErrorUnknown,
		Message:  "provider request failed",
		Cause:    err,
	}
}

func eventPayload(provider string, request Request, duration time.Duration) map[string]any {
	payload := map[string]any{
		"provider":    provider,
		"model":       strings.TrimSpace(request.Model),
		"duration_ms": duration.Milliseconds(),
	}
	for _, key := range []string{
		"project_id",
		"plan_id",
		"plan_version",
		"planning_context_id",
		"repository_summary_id",
	} {
		if value := strings.TrimSpace(request.Metadata[key]); value != "" {
			if key == "plan_version" {
				if number, err := strconv.Atoi(value); err == nil {
					payload[key] = number
					continue
				}
			}
			payload[key] = value
		}
	}
	return payload
}

func (g *GatewayService) record(ctx context.Context, eventType string, payload map[string]any) {
	if g == nil || g.recorder == nil {
		return
	}
	recordContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	createdAt := g.now()
	if err := g.recorder.Record(recordContext, "", eventType, payload, createdAt); err != nil {
		slog.Warn("record LLM gateway activity", "event_type", eventType, "error", err)
	}
}
