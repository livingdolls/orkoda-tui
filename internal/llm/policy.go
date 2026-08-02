package llm

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// FallbackTarget identifies an explicitly configured provider and model pair.
type FallbackTarget struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// TokenBudget bounds one logical gateway request across retries and fallbacks.
type TokenBudget struct {
	MaxInputTokens  int `json:"max_input_tokens"`
	MaxOutputTokens int `json:"max_output_tokens"`
	MaxTotalTokens  int `json:"max_total_tokens"`
}

// ExecutionPolicy controls retry, timeout, fallback, and token-budget behavior.
type ExecutionPolicy struct {
	AttemptTimeout time.Duration
	MaxWallClock   time.Duration
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Jitter         float64
	Fallbacks      []FallbackTarget
	Budget         TokenBudget
}

// PolicyInfo is the safe, read-only representation exposed through the daemon API.
type PolicyInfo struct {
	AttemptTimeoutMS int64            `json:"attempt_timeout_ms"`
	MaxWallClockMS   int64            `json:"max_wall_clock_ms"`
	MaxAttempts      int              `json:"max_attempts"`
	InitialBackoffMS int64            `json:"initial_backoff_ms"`
	MaxBackoffMS     int64            `json:"max_backoff_ms"`
	Jitter           float64          `json:"jitter"`
	Fallbacks        []FallbackTarget `json:"fallbacks"`
	Budget           TokenBudget      `json:"budget"`
}

// TokenEstimator estimates request size without claiming provider-specific accuracy.
type TokenEstimator interface {
	Estimate(Request) int
}

// ConservativeTokenEstimator uses a deterministic character-based approximation.
type ConservativeTokenEstimator struct{}

func (ConservativeTokenEstimator) Estimate(request Request) int {
	characters := len(request.ResponseSchema)
	for _, message := range request.Messages {
		characters += len(message.Content)
		characters += 24 // conservative role and message framing overhead
	}
	if characters == 0 {
		return 0
	}
	return int(math.Ceil(float64(characters) / 4.0))
}

func SingleAttemptPolicy() ExecutionPolicy {
	return ExecutionPolicy{MaxAttempts: 1}
}

func (p ExecutionPolicy) Info() PolicyInfo {
	fallbacks := make([]FallbackTarget, len(p.Fallbacks))
	copy(fallbacks, p.Fallbacks)
	return PolicyInfo{
		AttemptTimeoutMS: p.AttemptTimeout.Milliseconds(),
		MaxWallClockMS:   p.MaxWallClock.Milliseconds(),
		MaxAttempts:      p.MaxAttempts,
		InitialBackoffMS: p.InitialBackoff.Milliseconds(),
		MaxBackoffMS:     p.MaxBackoff.Milliseconds(),
		Jitter:           p.Jitter,
		Fallbacks:        fallbacks,
		Budget:           p.Budget,
	}
}

func (p ExecutionPolicy) validate(registry *Registry) (ExecutionPolicy, error) {
	if p.MaxAttempts < 1 {
		return ExecutionPolicy{}, fmt.Errorf("LLM max attempts must be at least one")
	}
	if p.AttemptTimeout < 0 || p.MaxWallClock < 0 {
		return ExecutionPolicy{}, fmt.Errorf("LLM timeouts cannot be negative")
	}
	if p.InitialBackoff < 0 || p.MaxBackoff < 0 {
		return ExecutionPolicy{}, fmt.Errorf("LLM backoff cannot be negative")
	}
	if p.MaxBackoff > 0 && p.InitialBackoff > p.MaxBackoff {
		return ExecutionPolicy{}, fmt.Errorf("LLM maximum backoff must not be smaller than initial backoff")
	}
	if p.Jitter < 0 || p.Jitter > 1 {
		return ExecutionPolicy{}, fmt.Errorf("LLM backoff jitter must be between zero and one")
	}
	if p.Budget.MaxInputTokens < 0 || p.Budget.MaxOutputTokens < 0 || p.Budget.MaxTotalTokens < 0 {
		return ExecutionPolicy{}, fmt.Errorf("LLM token budgets cannot be negative")
	}

	seen := make(map[string]struct{}, len(p.Fallbacks))
	fallbacks := make([]FallbackTarget, 0, len(p.Fallbacks))
	for _, fallback := range p.Fallbacks {
		fallback.Provider = normalizeProviderName(fallback.Provider)
		fallback.Model = strings.TrimSpace(fallback.Model)
		if fallback.Provider == "" || fallback.Model == "" {
			return ExecutionPolicy{}, fmt.Errorf("LLM fallback provider and model are required")
		}
		key := fallback.Provider + "\x00" + fallback.Model
		if _, exists := seen[key]; exists {
			return ExecutionPolicy{}, fmt.Errorf("duplicate LLM fallback target: %s/%s", fallback.Provider, fallback.Model)
		}
		seen[key] = struct{}{}
		if registry != nil {
			if _, err := registry.Provider(fallback.Provider); err != nil {
				return ExecutionPolicy{}, fmt.Errorf("LLM fallback provider %s is not registered: %w", fallback.Provider, err)
			}
		}
		fallbacks = append(fallbacks, fallback)
	}
	p.Fallbacks = fallbacks
	return p, nil
}
