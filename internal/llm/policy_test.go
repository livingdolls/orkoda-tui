package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixedEstimator int

func (f fixedEstimator) Estimate(Request) int { return int(f) }

type scriptedResult struct {
	response Response
	err      error
}

type scriptedProvider struct {
	name string
	mu   sync.Mutex
	rows []scriptedResult
}

func (p *scriptedProvider) Name() string { return p.name }

func (p *scriptedProvider) Complete(_ context.Context, _ Request) (Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.rows) == 0 {
		return Response{}, errors.New("script exhausted")
	}
	row := p.rows[0]
	p.rows = p.rows[1:]
	return row.response, row.err
}

func TestPolicyGatewayRetriesRateLimitAndHonorsRetryAfter(t *testing.T) {
	fake, err := NewFakeProvider("primary",
		FakeResult{Err: NewFakeRateLimitError("primary", 2*time.Second)},
		FakeResult{Response: Response{ID: "ok", Content: "{}", Usage: Usage{InputTokens: 10, OutputTokens: 4}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(fake)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := NewPolicyGateway(registry, &eventRecorder{}, ExecutionPolicy{
		MaxAttempts:    2,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     time.Second,
	}, fixedEstimator(20))
	if err != nil {
		t.Fatal(err)
	}
	var delays []time.Duration
	gateway.sleep = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}
	gateway.random = func() float64 { return 0.5 }

	response, err := gateway.Complete(context.Background(), "primary", validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Requests()) != 2 || len(delays) != 1 || delays[0] != 2*time.Second {
		t.Fatalf("unexpected retry state: requests=%d delays=%v", len(fake.Requests()), delays)
	}
	if response.Usage.AttemptCount != 2 || response.Usage.FallbackUsed {
		t.Fatalf("unexpected execution metadata: %#v", response.Usage)
	}
}

func TestPolicyGatewayFallsBackAfterUnavailable(t *testing.T) {
	primary, _ := NewFakeProvider("primary", FakeResult{Err: &ProviderError{
		Provider: "primary", Code: ErrorUnavailable, Message: "down", Retryable: true,
	}})
	secondary, _ := NewFakeProvider("secondary", FakeResult{Response: Response{
		ID: "secondary-result", Content: "{}", Usage: Usage{InputTokens: 12, OutputTokens: 3},
	}})
	registry, err := NewRegistry(primary, secondary)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := NewPolicyGateway(registry, nil, ExecutionPolicy{
		MaxAttempts: 2,
		Fallbacks: []FallbackTarget{{Provider: "secondary", Model: "secondary-model"}},
	}, fixedEstimator(10))
	if err != nil {
		t.Fatal(err)
	}
	gateway.sleep = func(context.Context, time.Duration) error { return nil }

	response, err := gateway.Complete(context.Background(), "primary", validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(primary.Requests()) != 1 || len(secondary.Requests()) != 1 {
		t.Fatalf("unexpected provider calls: primary=%d secondary=%d", len(primary.Requests()), len(secondary.Requests()))
	}
	if !response.Usage.FallbackUsed || response.Usage.FinalProvider != "secondary" || response.Usage.FinalModel != "secondary-model" {
		t.Fatalf("unexpected fallback metadata: %#v", response.Usage)
	}
}

func TestPolicyGatewayDoesNotRetryAuthentication(t *testing.T) {
	fake, _ := NewFakeProvider("primary", FakeResult{Err: &ProviderError{
		Provider: "primary", Code: ErrorAuthentication, Message: "bad key",
	}})
	registry, _ := NewRegistry(fake)
	gateway, err := NewPolicyGateway(registry, nil, ExecutionPolicy{MaxAttempts: 3}, fixedEstimator(10))
	if err != nil {
		t.Fatal(err)
	}
	_, err = gateway.Complete(context.Background(), "primary", validRequest())
	assertProviderCode(t, err, ErrorAuthentication)
	if len(fake.Requests()) != 1 {
		t.Fatalf("authentication failure was retried %d times", len(fake.Requests()))
	}
}

func TestPolicyGatewayCancelsDuringBackoff(t *testing.T) {
	fake, _ := NewFakeProvider("primary", FakeResult{Err: NewFakeRateLimitError("primary", time.Second)})
	registry, _ := NewRegistry(fake)
	gateway, err := NewPolicyGateway(registry, nil, ExecutionPolicy{
		MaxAttempts:    2,
		InitialBackoff: time.Second,
		MaxBackoff:     time.Second,
	}, fixedEstimator(10))
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	gateway.sleep = func(ctx context.Context, _ time.Duration) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, callErr := gateway.Complete(ctx, "primary", validRequest())
		result <- callErr
	}()
	<-entered
	cancel()
	assertProviderCode(t, <-result, ErrorCancelled)
}

func TestPolicyGatewayUsesParentDeadline(t *testing.T) {
	fake, _ := NewFakeProvider("primary", FakeResult{Response: Response{Content: "{}"}, Delay: time.Second})
	registry, _ := NewRegistry(fake)
	gateway, err := NewPolicyGateway(registry, nil, ExecutionPolicy{
		MaxAttempts:    1,
		AttemptTimeout: time.Second,
	}, fixedEstimator(10))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err = gateway.Complete(ctx, "primary", validRequest())
	assertProviderCode(t, err, ErrorTimeout)
}

func TestPolicyGatewayRejectsAndExhaustsBudget(t *testing.T) {
	fake, _ := NewFakeProvider("primary", FakeResult{Response: Response{Content: "{}"}})
	registry, _ := NewRegistry(fake)
	gateway, err := NewPolicyGateway(registry, nil, ExecutionPolicy{
		MaxAttempts: 1,
		Budget:      TokenBudget{MaxInputTokens: 50},
	}, fixedEstimator(100))
	if err != nil {
		t.Fatal(err)
	}
	_, err = gateway.Complete(context.Background(), "primary", validRequest())
	assertProviderCode(t, err, ErrorBudgetExceeded)
	if len(fake.Requests()) != 0 {
		t.Fatal("provider was called after budget preflight rejection")
	}

	fake, _ = NewFakeProvider("primary", FakeResult{Err: &ProviderError{
		Provider: "primary", Code: ErrorUnavailable, Message: "down", Retryable: true,
	}})
	registry, _ = NewRegistry(fake)
	gateway, err = NewPolicyGateway(registry, nil, ExecutionPolicy{
		MaxAttempts: 2,
		Budget:      TokenBudget{MaxOutputTokens: 50, MaxTotalTokens: 200},
	}, fixedEstimator(100))
	if err != nil {
		t.Fatal(err)
	}
	gateway.sleep = func(context.Context, time.Duration) error { return nil }
	request := validRequest()
	request.MaxOutputTokens = 50
	_, err = gateway.Complete(context.Background(), "primary", request)
	assertProviderCode(t, err, ErrorBudgetExceeded)
	if len(fake.Requests()) != 1 {
		t.Fatalf("expected budget exhaustion before retry, got %d calls", len(fake.Requests()))
	}
}

func TestPolicyGatewayAggregatesUsageAcrossAttempts(t *testing.T) {
	provider := &scriptedProvider{name: "primary", rows: []scriptedResult{
		{
			response: Response{Usage: Usage{InputTokens: 5, OutputTokens: 1}},
			err: &ProviderError{Provider: "primary", Code: ErrorUnavailable, Message: "retry", Retryable: true},
		},
		{response: Response{Content: "{}", Usage: Usage{InputTokens: 7, OutputTokens: 2}}},
	}}
	registry, _ := NewRegistry(provider)
	gateway, err := NewPolicyGateway(registry, nil, ExecutionPolicy{MaxAttempts: 2}, fixedEstimator(10))
	if err != nil {
		t.Fatal(err)
	}
	gateway.sleep = func(context.Context, time.Duration) error { return nil }
	response, err := gateway.Complete(context.Background(), "primary", validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Usage.InputTokens != 12 || response.Usage.OutputTokens != 3 || response.Usage.TotalTokens != 15 {
		t.Fatalf("usage was not aggregated: %#v", response.Usage)
	}
}

func TestPolicyValidationRejectsUnknownDuplicateAndPrimaryFallbacks(t *testing.T) {
	primary, _ := NewFakeProvider("primary")
	secondary, _ := NewFakeProvider("secondary")
	registry, _ := NewRegistry(primary, secondary)
	_, err := NewPolicyGateway(registry, nil, ExecutionPolicy{
		MaxAttempts: 2,
		Fallbacks:   []FallbackTarget{{Provider: "missing", Model: "model"}},
	}, fixedEstimator(1))
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected unknown fallback validation error, got %v", err)
	}
	_, err = NewPolicyGateway(registry, nil, ExecutionPolicy{
		MaxAttempts: 2,
		Fallbacks: []FallbackTarget{
			{Provider: "secondary", Model: "model"},
			{Provider: "secondary", Model: "model"},
		},
	}, fixedEstimator(1))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate fallback validation error, got %v", err)
	}
	gateway, err := NewPolicyGateway(registry, nil, ExecutionPolicy{
		MaxAttempts: 2,
		Fallbacks:   []FallbackTarget{{Provider: "primary", Model: "other"}},
	}, fixedEstimator(1))
	if err != nil {
		t.Fatal(err)
	}
	_, err = gateway.Complete(context.Background(), "primary", validRequest())
	assertProviderCode(t, err, ErrorInvalidRequest)
}
