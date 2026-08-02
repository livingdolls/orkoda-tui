package llm

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SafetyPolicy struct {
	RedactionMode              RedactionMode
	MaxRepairAttempts          int
	MaxStructuredResponseBytes int
}

type SafetyGateway struct {
	inner     Gateway
	recorder  EventRecorder
	redactor  PromptRedactor
	validator StructuredValidator
	estimator TokenEstimator
	policy    SafetyPolicy
	now       func() time.Time
}

func NewSafetyGateway(
	inner Gateway,
	recorder EventRecorder,
	policy SafetyPolicy,
	redactor PromptRedactor,
	validator StructuredValidator,
	estimator TokenEstimator,
) (*SafetyGateway, error) {
	if inner == nil {
		return nil, fmt.Errorf("LLM gateway is required")
	}
	validated, err := policy.validate()
	if err != nil {
		return nil, err
	}
	if redactor == nil {
		redactor = NewStandardRedactor()
	}
	if validator == nil {
		validator = JSONSchemaValidator{}
	}
	if estimator == nil {
		estimator = ConservativeTokenEstimator{}
	}
	return &SafetyGateway{
		inner:     inner,
		recorder:  recorder,
		redactor:  redactor,
		validator: validator,
		estimator: estimator,
		policy:    validated,
		now:       func() time.Time { return time.Now().UTC() },
	}, nil
}

func (p SafetyPolicy) validate() (SafetyPolicy, error) {
	if p.RedactionMode == "" {
		p.RedactionMode = RedactionModeStrict
	}
	if p.RedactionMode != RedactionModeStrict && p.RedactionMode != RedactionModeReport && p.RedactionMode != RedactionModeOff {
		return SafetyPolicy{}, fmt.Errorf("unsupported LLM redaction mode %q", p.RedactionMode)
	}
	if p.MaxRepairAttempts < 0 {
		return SafetyPolicy{}, fmt.Errorf("LLM maximum repair attempts cannot be negative")
	}
	if p.MaxStructuredResponseBytes <= 0 {
		return SafetyPolicy{}, fmt.Errorf("LLM maximum structured response size must be positive")
	}
	return p, nil
}

func (g *SafetyGateway) Info() PolicyInfo {
	info := PolicyInfo{}
	if reader, ok := g.inner.(interface{ Info() PolicyInfo }); ok {
		info = reader.Info()
	}
	info.RedactionMode = string(g.policy.RedactionMode)
	info.StructuredValidation = true
	info.MaxRepairAttempts = g.policy.MaxRepairAttempts
	info.MaxStructuredResponseBytes = g.policy.MaxStructuredResponseBytes
	return info
}

func (g *SafetyGateway) Complete(ctx context.Context, provider string, request Request) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	safeRequest, report, err := g.redactor.Redact(request, g.policy.RedactionMode)
	if err != nil {
		return Response{}, &ProviderError{
			Provider: provider,
			Code:     ErrorRedactionFailed,
			Message:  "prompt redaction failed",
			Cause:    err,
		}
	}
	if report.Count > 0 {
		payload := eventPayload(provider, safeRequest, 0)
		payload["redaction_mode"] = report.Mode
		payload["redaction_count"] = report.Count
		payload["redaction_types"] = safeRedactionTypes(report.Types)
		g.record(ctx, "llm.prompt_redacted", payload)
	}

	runContext := ctx
	cancel := func() {}
	if info := g.Info(); info.MaxWallClockMS > 0 {
		runContext, cancel = context.WithTimeout(ctx, time.Duration(info.MaxWallClockMS)*time.Millisecond)
	}
	defer cancel()

	currentRequest := cloneRequest(safeRequest)
	var aggregate Usage
	validationAttempts := 0
	validationErrorCount := 0
	repairUsed := false

	for repairAttempt := 0; ; repairAttempt++ {
		response, callErr := g.inner.Complete(runContext, provider, currentRequest)
		if callErr != nil {
			if repairAttempt > 0 {
				payload := eventPayload(provider, currentRequest, 0)
				payload["repair_attempt"] = repairAttempt
				if providerError, ok := AsProviderError(callErr); ok {
					payload["error_code"] = providerError.Code
				}
				g.record(runContext, "llm.output_repair_failed", payload)
			}
			return Response{}, callErr
		}

		aggregate = mergeSafetyUsage(aggregate, response.Usage)
		validationAttempts++
		g.recordValidationStarted(runContext, provider, currentRequest, validationAttempts)
		normalized, issues := g.validateResponse(response.Content, currentRequest.ResponseSchema)
		if len(issues) == 0 {
			response.Content = string(normalized)
			response.Usage = aggregate
			response.Usage.ValidationAttempts = validationAttempts
			response.Usage.ValidationErrorCount = validationErrorCount
			response.Usage.RepairUsed = repairUsed
			response.Usage.RedactionCount = report.Count
			response.Metadata = safetyMetadata(
				response.Metadata,
				validationAttempts,
				validationErrorCount,
				repairUsed,
				report.Count,
			)
			if repairAttempt > 0 {
				payload := eventPayload(provider, currentRequest, 0)
				payload["repair_attempt"] = repairAttempt
				payload["validation_attempts"] = validationAttempts
				g.record(runContext, "llm.output_repair_completed", payload)
			}
			return cloneResponse(response), nil
		}

		validationErrorCount += len(issues)
		g.recordValidationFailed(runContext, provider, currentRequest, validationAttempts, issues)
		if repairAttempt >= g.policy.MaxRepairAttempts {
			return Response{}, structuredValidationError(provider, issues)
		}

		repairUsed = true
		repairRequest := buildRepairRequest(safeRequest, response.Model, issues)
		repairRequest, budgetErr := g.applyRepairBudget(repairRequest, aggregate)
		if budgetErr != nil {
			payload := eventPayload(provider, repairRequest, 0)
			payload["repair_attempt"] = repairAttempt + 1
			payload["error_code"] = ErrorBudgetExceeded
			payload["total_tokens_so_far"] = aggregate.TotalTokens
			g.record(runContext, "llm.budget_exhausted", payload)
			return Response{}, budgetErr
		}
		payload := eventPayload(provider, repairRequest, 0)
		payload["repair_attempt"] = repairAttempt + 1
		payload["validation_error_count"] = len(issues)
		g.record(runContext, "llm.output_repair_started", payload)
		currentRequest = repairRequest
	}
}

func (g *SafetyGateway) validateResponse(content string, schema []byte) (jsonContent []byte, issues []ValidationIssue) {
	if len(content) > g.policy.MaxStructuredResponseBytes {
		return nil, []ValidationIssue{{
			Path:    "$",
			Code:    "response_too_large",
			Message: "structured response exceeds the configured size limit",
		}}
	}
	normalized, validationIssues := g.validator.Validate(schema, content)
	return normalized, validationIssues
}

func (g *SafetyGateway) applyRepairBudget(request Request, aggregate Usage) (Request, *ProviderError) {
	info := g.Info()
	estimatedInput := g.estimator.Estimate(request)
	if info.Budget.MaxInputTokens > 0 && estimatedInput > info.Budget.MaxInputTokens {
		return request, budgetError("repair request exceeds the configured input token budget")
	}
	if info.Budget.MaxOutputTokens > 0 && (request.MaxOutputTokens <= 0 || request.MaxOutputTokens > info.Budget.MaxOutputTokens) {
		request.MaxOutputTokens = info.Budget.MaxOutputTokens
	}
	if info.Budget.MaxTotalTokens > 0 {
		remaining := info.Budget.MaxTotalTokens - aggregate.TotalTokens - estimatedInput
		if remaining <= 0 {
			return request, budgetError("remaining token budget is insufficient for structured output repair")
		}
		if request.MaxOutputTokens <= 0 || request.MaxOutputTokens > remaining {
			request.MaxOutputTokens = remaining
		}
	}
	return request, nil
}

func buildRepairRequest(original Request, responseModel string, issues []ValidationIssue) Request {
	repair := cloneRequest(original)
	if model := strings.TrimSpace(responseModel); model != "" {
		repair.Model = model
	}
	var prompt strings.Builder
	prompt.WriteString("The previous response failed structured output validation. Return a corrected JSON object matching the response schema exactly. Do not include Markdown or prose outside JSON.\nValidation issues:\n")
	for _, issue := range issues {
		prompt.WriteString("- ")
		prompt.WriteString(issue.Path)
		prompt.WriteString(" [")
		prompt.WriteString(issue.Code)
		prompt.WriteString("]: ")
		prompt.WriteString(issue.Message)
		prompt.WriteByte('\n')
	}
	repair.Messages = append(repair.Messages, Message{Role: RoleUser, Content: strings.TrimSpace(prompt.String())})
	return repair
}

func structuredValidationError(provider string, issues []ValidationIssue) *ProviderError {
	code := ErrorStructuredOutputInvalid
	message := "provider returned an invalid structured response"
	if len(issues) > 0 {
		if issues[0].Code == "response_too_large" {
			code = ErrorStructuredOutputTooLarge
		}
		message = fmt.Sprintf("structured output validation failed: %s %s", issues[0].Path, issues[0].Message)
	}
	return &ProviderError{Provider: provider, Code: code, Message: message}
}

func mergeSafetyUsage(total Usage, next Usage) Usage {
	merged := addUsage(total, next)
	merged.AttemptCount = total.AttemptCount + next.AttemptCount
	merged.FallbackUsed = total.FallbackUsed || next.FallbackUsed
	merged.FinalProvider = next.FinalProvider
	merged.FinalModel = next.FinalModel
	merged.EstimatedInputTokens = total.EstimatedInputTokens + next.EstimatedInputTokens
	merged.EstimatedTokensSpent = total.EstimatedTokensSpent + next.EstimatedTokensSpent
	return merged
}

func safetyMetadata(
	metadata map[string]string,
	validationAttempts int,
	validationErrorCount int,
	repairUsed bool,
	redactionCount int,
) map[string]string {
	result := cloneStrings(metadata)
	if result == nil {
		result = map[string]string{}
	}
	result["validation_attempts"] = strconv.Itoa(validationAttempts)
	result["validation_error_count"] = strconv.Itoa(validationErrorCount)
	result["repair_used"] = strconv.FormatBool(repairUsed)
	result["redaction_count"] = strconv.Itoa(redactionCount)
	return result
}

func (g *SafetyGateway) recordValidationStarted(ctx context.Context, provider string, request Request, attempt int) {
	payload := eventPayload(provider, request, 0)
	payload["validation_attempt"] = attempt
	g.record(ctx, "llm.output_validation_started", payload)
}

func (g *SafetyGateway) recordValidationFailed(
	ctx context.Context,
	provider string,
	request Request,
	attempt int,
	issues []ValidationIssue,
) {
	payload := eventPayload(provider, request, 0)
	payload["validation_attempt"] = attempt
	payload["validation_error_count"] = len(issues)
	if len(issues) > 0 {
		payload["validation_error_code"] = issues[0].Code
		payload["validation_error_path"] = issues[0].Path
	}
	g.record(ctx, "llm.output_validation_failed", payload)
}

func (g *SafetyGateway) record(ctx context.Context, eventType string, payload map[string]any) {
	if g == nil || g.recorder == nil {
		return
	}
	recordContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = g.recorder.Record(recordContext, "", eventType, payload, g.now())
}
