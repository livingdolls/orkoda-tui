package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type GatewayService struct {
	registry          *Registry
	recorder          EventRecorder
	policy            ExecutionPolicy
	estimator         TokenEstimator
	emitAttemptEvents bool
	now               func() time.Time
	sleep             func(context.Context, time.Duration) error
	random            func() float64
	sequence          atomic.Uint64
}

func NewGateway(registry *Registry, recorder EventRecorder) (*GatewayService, error) {
	return newGateway(registry, recorder, SingleAttemptPolicy(), ConservativeTokenEstimator{}, false)
}

func NewPolicyGateway(
	registry *Registry,
	recorder EventRecorder,
	policy ExecutionPolicy,
	estimator TokenEstimator,
) (*GatewayService, error) {
	return newGateway(registry, recorder, policy, estimator, true)
}

func newGateway(
	registry *Registry,
	recorder EventRecorder,
	policy ExecutionPolicy,
	estimator TokenEstimator,
	emitAttemptEvents bool,
) (*GatewayService, error) {
	if registry == nil {
		return nil, fmt.Errorf("LLM provider registry is required")
	}
	validatedPolicy, err := policy.validate(registry)
	if err != nil {
		return nil, err
	}
	if estimator == nil {
		estimator = ConservativeTokenEstimator{}
	}
	return &GatewayService{
		registry:          registry,
		recorder:          recorder,
		policy:            validatedPolicy,
		estimator:         estimator,
		emitAttemptEvents: emitAttemptEvents,
		now:               func() time.Time { return time.Now().UTC() },
		sleep:             sleepContext,
		random:            rand.Float64,
	}, nil
}

func (g *GatewayService) Info() PolicyInfo {
	if g == nil {
		return PolicyInfo{}
	}
	return g.policy.Info()
}

func (g *GatewayService) Complete(ctx context.Context, providerName string, request Request) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	providerName = normalizeProviderName(providerName)
	if _, err := g.registry.Provider(providerName); err != nil {
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
	for _, fallback := range g.policy.Fallbacks {
		if fallback.Provider == providerName {
			return Response{}, &ProviderError{
				Provider: providerName,
				Code:     ErrorInvalidRequest,
				Message:  "fallback provider must differ from the primary provider",
			}
		}
	}

	request = cloneRequest(request)
	request, estimatedInput, err := g.applyBudget(request)
	requestID := g.requestID()
	if err != nil {
		payload := eventPayload(providerName, request, 0)
		payload["request_id"] = requestID
		payload["estimated_input_tokens"] = estimatedInput
		payload["error_code"] = ErrorBudgetExceeded
		g.record(ctx, "llm.budget_rejected", payload)
		return Response{}, err
	}

	runContext := ctx
	cancelRun := func() {}
	if g.policy.MaxWallClock > 0 {
		runContext, cancelRun = context.WithTimeout(ctx, g.policy.MaxWallClock)
	}
	defer cancelRun()

	startedAt := g.now()
	startPayload := eventPayload(providerName, request, 0)
	startPayload["request_id"] = requestID
	startPayload["estimated_input_tokens"] = estimatedInput
	startPayload["max_attempts"] = g.policy.MaxAttempts
	g.record(runContext, "llm.request_started", startPayload)

	targets := make([]FallbackTarget, 0, len(g.policy.Fallbacks)+1)
	targets = append(targets, FallbackTarget{Provider: providerName, Model: request.Model})
	targets = append(targets, g.policy.Fallbacks...)

	var aggregate Usage
	targetIndex := 0
	fallbackUsed := false
	estimatedSpent := 0
	var lastError *ProviderError
	attempts := 0

	for attempts < g.policy.MaxAttempts {
		if err := runContext.Err(); err != nil {
			return Response{}, g.finishFailure(runContext, requestID, providerName, request, startedAt, attempts, fallbackUsed, aggregate, normalizeProviderError(providerName, err))
		}
		if err := g.checkRemainingBudget(request, estimatedInput, estimatedSpent, aggregate); err != nil {
			payload := eventPayload(providerName, request, g.elapsed(startedAt))
			payload["request_id"] = requestID
			payload["attempt_count"] = attempts
			payload["total_tokens_so_far"] = aggregate.TotalTokens
			payload["estimated_tokens_spent"] = estimatedSpent
			payload["error_code"] = ErrorBudgetExceeded
			g.record(runContext, "llm.budget_exhausted", payload)
			return Response{}, g.finishFailure(runContext, requestID, providerName, request, startedAt, attempts, fallbackUsed, aggregate, err)
		}

		attempts++
		target := targets[targetIndex]
		attemptRequest := cloneRequest(request)
		attemptRequest.Model = target.Model
		provider, lookupErr := g.registry.Provider(target.Provider)
		if lookupErr != nil {
			lastError = &ProviderError{
				Provider: target.Provider,
				Code:     ErrorUnavailable,
				Message:  "provider is not registered",
				Cause:    lookupErr,
			}
			break
		}

		attemptStarted := g.now()
		if g.emitAttemptEvents {
			payload := eventPayload(target.Provider, attemptRequest, 0)
			payload["request_id"] = requestID
			payload["attempt"] = attempts
			payload["fallback"] = targetIndex > 0
			payload["estimated_input_tokens"] = estimatedInput
			g.record(runContext, "llm.attempt_started", payload)
		}

		attemptContext := runContext
		cancelAttempt := func() {}
		if g.policy.AttemptTimeout > 0 {
			attemptContext, cancelAttempt = context.WithTimeout(runContext, g.policy.AttemptTimeout)
		}
		response, callErr := provider.Complete(attemptContext, attemptRequest)
		attemptContextErr := attemptContext.Err()
		cancelAttempt()
		if attemptContextErr != nil {
			callErr = attemptContextErr
		}
		response = normalizeResponse(attemptRequest, response)
		if hasUsage(response.Usage) {
			aggregate = addUsage(aggregate, response.Usage)
		} else if callErr != nil {
			estimatedSpent += estimatedInput
		}

		if callErr == nil {
			if budgetErr := g.checkActualBudget(target.Provider, aggregate); budgetErr != nil {
				payload := eventPayload(target.Provider, attemptRequest, g.elapsed(startedAt))
				payload["request_id"] = requestID
				payload["attempt_count"] = attempts
				payload["total_tokens_so_far"] = aggregate.TotalTokens
				payload["error_code"] = ErrorBudgetExceeded
				g.record(runContext, "llm.budget_exhausted", payload)
				return Response{}, g.finishFailure(runContext, requestID, providerName, request, startedAt, attempts, fallbackUsed, aggregate, budgetErr)
			}
			if g.emitAttemptEvents {
				payload := eventPayload(target.Provider, attemptRequest, g.elapsed(attemptStarted))
				payload["request_id"] = requestID
				payload["attempt"] = attempts
				payload["input_tokens"] = response.Usage.InputTokens
				payload["output_tokens"] = response.Usage.OutputTokens
				payload["total_tokens"] = response.Usage.TotalTokens
				g.record(runContext, "llm.attempt_completed", payload)
			}
			response.Usage = aggregate
			response.Metadata = executionMetadata(response.Metadata, providerName, request.Model, target, requestID, attempts, fallbackUsed, estimatedInput, estimatedSpent)
			payload := eventPayload(target.Provider, attemptRequest, g.elapsed(startedAt))
			payload["request_id"] = requestID
			payload["response_id"] = response.ID
			payload["finish_reason"] = response.FinishReason
			payload["attempt_count"] = attempts
			payload["fallback_used"] = fallbackUsed
			payload["input_tokens"] = aggregate.InputTokens
			payload["output_tokens"] = aggregate.OutputTokens
			payload["cached_input_tokens"] = aggregate.CachedInputTokens
			payload["total_tokens"] = aggregate.TotalTokens
			g.record(runContext, "llm.request_completed", payload)
			return cloneResponse(response), nil
		}

		lastError = normalizeProviderError(target.Provider, callErr)
		if g.emitAttemptEvents {
			payload := eventPayload(target.Provider, attemptRequest, g.elapsed(attemptStarted))
			payload["request_id"] = requestID
			payload["attempt"] = attempts
			payload["error_code"] = lastError.Code
			payload["retryable"] = lastError.Retryable
			payload["total_tokens_so_far"] = aggregate.TotalTokens
			g.record(runContext, "llm.attempt_failed", payload)
		}
		if !retryableError(lastError) || attempts >= g.policy.MaxAttempts {
			break
		}

		if lastError.Code != ErrorRateLimited && targetIndex+1 < len(targets) {
			targetIndex++
			fallbackUsed = true
			selected := targets[targetIndex]
			payload := eventPayload(selected.Provider, Request{Model: selected.Model, Metadata: request.Metadata}, 0)
			payload["request_id"] = requestID
			payload["attempt"] = attempts + 1
			payload["from_provider"] = target.Provider
			g.record(runContext, "llm.fallback_selected", payload)
		}

		delay := g.retryDelay(attempts, lastError.RetryAfter)
		payload := eventPayload(targets[targetIndex].Provider, Request{Model: targets[targetIndex].Model, Metadata: request.Metadata}, 0)
		payload["request_id"] = requestID
		payload["attempt"] = attempts + 1
		payload["delay_ms"] = delay.Milliseconds()
		payload["error_code"] = lastError.Code
		if lastError.RetryAfter > 0 {
			payload["retry_after_ms"] = lastError.RetryAfter.Milliseconds()
		}
		g.record(runContext, "llm.retry_scheduled", payload)
		if err := g.sleep(runContext, delay); err != nil {
			lastError = normalizeProviderError(targets[targetIndex].Provider, err)
			break
		}
	}

	if lastError == nil {
		lastError = &ProviderError{
			Provider: providerName,
			Code:     ErrorUnknown,
			Message:  "provider request failed",
		}
	}
	return Response{}, g.finishFailure(runContext, requestID, providerName, request, startedAt, attempts, fallbackUsed, aggregate, lastError)
}

func (g *GatewayService) applyBudget(request Request) (Request, int, *ProviderError) {
	estimated := g.estimator.Estimate(request)
	budget := g.policy.Budget
	if budget.MaxInputTokens > 0 && estimated > budget.MaxInputTokens {
		return request, estimated, budgetError("estimated input exceeds the configured token budget")
	}
	if budget.MaxOutputTokens > 0 && (request.MaxOutputTokens <= 0 || request.MaxOutputTokens > budget.MaxOutputTokens) {
		request.MaxOutputTokens = budget.MaxOutputTokens
	}
	if budget.MaxTotalTokens > 0 {
		remaining := budget.MaxTotalTokens - estimated
		if remaining <= 0 {
			return request, estimated, budgetError("estimated request exceeds the configured total token budget")
		}
		if request.MaxOutputTokens <= 0 || request.MaxOutputTokens > remaining {
			request.MaxOutputTokens = remaining
		}
	}
	return request, estimated, nil
}

func (g *GatewayService) checkRemainingBudget(request Request, estimatedInput, estimatedSpent int, usage Usage) *ProviderError {
	budget := g.policy.Budget
	if budget.MaxTotalTokens == 0 {
		return nil
	}
	required := estimatedInput + max(0, request.MaxOutputTokens)
	remaining := budget.MaxTotalTokens - usage.TotalTokens - estimatedSpent
	if required > remaining {
		return budgetError("remaining token budget is insufficient for another attempt")
	}
	return nil
}

func (g *GatewayService) checkActualBudget(provider string, usage Usage) *ProviderError {
	budget := g.policy.Budget
	if budget.MaxInputTokens > 0 && usage.InputTokens > budget.MaxInputTokens {
		return budgetErrorFor(provider, "actual input usage exceeded the configured token budget")
	}
	if budget.MaxOutputTokens > 0 && usage.OutputTokens > budget.MaxOutputTokens {
		return budgetErrorFor(provider, "actual output usage exceeded the configured token budget")
	}
	if budget.MaxTotalTokens > 0 && usage.TotalTokens > budget.MaxTotalTokens {
		return budgetErrorFor(provider, "actual total usage exceeded the configured token budget")
	}
	return nil
}

func (g *GatewayService) finishFailure(
	ctx context.Context,
	requestID, requestedProvider string,
	request Request,
	startedAt time.Time,
	attempts int,
	fallbackUsed bool,
	usage Usage,
	providerError *ProviderError,
) *ProviderError {
	if providerError == nil {
		providerError = &ProviderError{Provider: requestedProvider, Code: ErrorUnknown, Message: "provider request failed"}
	}
	payload := eventPayload(providerError.Provider, request, g.elapsed(startedAt))
	payload["request_id"] = requestID
	payload["attempt_count"] = attempts
	payload["fallback_used"] = fallbackUsed
	payload["error_code"] = providerError.Code
	payload["retryable"] = providerError.Retryable
	payload["input_tokens"] = usage.InputTokens
	payload["output_tokens"] = usage.OutputTokens
	payload["total_tokens"] = usage.TotalTokens
	if providerError.RetryAfter > 0 {
		payload["retry_after_ms"] = providerError.RetryAfter.Milliseconds()
	}
	eventType := "llm.request_failed"
	if providerError.Code == ErrorCancelled {
		eventType = "llm.request_cancelled"
	}
	g.record(ctx, eventType, payload)
	return providerError
}

func (g *GatewayService) retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	backoff := g.policy.InitialBackoff
	if backoff > 0 && attempt > 1 {
		factor := math.Pow(2, float64(attempt-1))
		backoff = time.Duration(float64(backoff) * factor)
	}
	if g.policy.MaxBackoff > 0 && backoff > g.policy.MaxBackoff {
		backoff = g.policy.MaxBackoff
	}
	if backoff > 0 && g.policy.Jitter > 0 {
		factor := 1 + ((g.random()*2 - 1) * g.policy.Jitter)
		backoff = time.Duration(math.Max(0, float64(backoff)*factor))
	}
	if retryAfter > backoff {
		return retryAfter
	}
	return backoff
}

func (g *GatewayService) elapsed(startedAt time.Time) time.Duration {
	duration := g.now().Sub(startedAt)
	if duration < 0 {
		return 0
	}
	return duration
}

func (g *GatewayService) requestID() string {
	return fmt.Sprintf("llm-%d-%d", g.now().UnixNano(), g.sequence.Add(1))
}

func executionMetadata(
	metadata map[string]string,
	requestedProvider, requestedModel string,
	finalTarget FallbackTarget,
	requestID string,
	attempts int,
	fallbackUsed bool,
	estimatedInput, estimatedSpent int,
) map[string]string {
	result := cloneStrings(metadata)
	if result == nil {
		result = map[string]string{}
	}
	result["request_id"] = requestID
	result["requested_provider"] = requestedProvider
	result["requested_model"] = strings.TrimSpace(requestedModel)
	result["final_provider"] = finalTarget.Provider
	result["final_model"] = finalTarget.Model
	result["attempt_count"] = strconv.Itoa(attempts)
	result["fallback_used"] = strconv.FormatBool(fallbackUsed)
	result["estimated_input_tokens"] = strconv.Itoa(estimatedInput)
	result["estimated_tokens_spent"] = strconv.Itoa(estimatedSpent)
	return result
}

func retryableError(providerError *ProviderError) bool {
	if providerError == nil || !providerError.Retryable {
		return false
	}
	switch providerError.Code {
	case ErrorRateLimited, ErrorTimeout, ErrorUnavailable:
		return true
	default:
		return false
	}
}

func hasUsage(usage Usage) bool {
	return usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.CachedInputTokens != 0 || usage.TotalTokens != 0
}

func addUsage(left, right Usage) Usage {
	return normalizeUsage(Usage{
		InputTokens:       left.InputTokens + right.InputTokens,
		OutputTokens:      left.OutputTokens + right.OutputTokens,
		CachedInputTokens: left.CachedInputTokens + right.CachedInputTokens,
	})
}

func budgetError(message string) *ProviderError {
	return budgetErrorFor("LLM", message)
}

func budgetErrorFor(provider, message string) *ProviderError {
	return &ProviderError{
		Provider: provider,
		Code:     ErrorBudgetExceeded,
		Message:  message,
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
