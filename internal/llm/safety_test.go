package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSafetyGatewayRedactsValidatesAndRepairs(t *testing.T) {
	fake, err := NewFakeProvider("fake",
		FakeResult{Response: Response{
			Model:   "fake-model",
			Content: `{"summary":1,"steps":[]}`,
			Usage:   Usage{InputTokens: 10, OutputTokens: 4},
		}},
		FakeResult{Response: Response{
			Model:   "fake-model",
			Content: `{"summary":"fixed","steps":["one"]}`,
			Usage:   Usage{InputTokens: 12, OutputTokens: 5},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	registry, _ := NewRegistry(fake)
	recorder := &eventRecorder{}
	inner, err := NewGateway(registry, recorder)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := NewSafetyGateway(
		inner,
		recorder,
		SafetyPolicy{
			RedactionMode:              RedactionModeStrict,
			MaxRepairAttempts:          1,
			MaxStructuredResponseBytes: 4096,
		},
		NewStandardRedactor(),
		JSONSchemaValidator{},
		ConservativeTokenEstimator{},
	)
	if err != nil {
		t.Fatal(err)
	}

	request := validRequest()
	request.Messages[1].Content = "Authorization: Bearer sk-abcdefghijklmnopqrstuvwxyz123456"
	request.ResponseSchema = validationTestSchema
	response, err := gateway.Complete(context.Background(), "fake", request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != `{"steps":["one"],"summary":"fixed"}` && response.Content != `{"summary":"fixed","steps":["one"]}` {
		t.Fatalf("unexpected repaired response: %s", response.Content)
	}
	if response.Usage.ValidationAttempts != 2 || !response.Usage.RepairUsed || response.Usage.RedactionCount != 1 {
		t.Fatalf("unexpected safety metadata: %#v", response.Usage)
	}
	if response.Usage.InputTokens != 22 || response.Usage.OutputTokens != 9 {
		t.Fatalf("repair usage was not aggregated: %#v", response.Usage)
	}

	requests := fake.Requests()
	if len(requests) != 2 {
		t.Fatalf("expected initial request and one repair, got %d", len(requests))
	}
	for _, captured := range requests {
		encoded, _ := json.Marshal(captured)
		if strings.Contains(string(encoded), "abcdefghijklmnopqrstuvwxyz123456") {
			t.Fatalf("provider received an unredacted token: %s", encoded)
		}
	}
	repairPrompt := requests[1].Messages[len(requests[1].Messages)-1].Content
	if !strings.Contains(repairPrompt, "$.summary [type]") || strings.Contains(repairPrompt, `{"summary":1`) {
		t.Fatalf("repair prompt was not safely derived from issues: %s", repairPrompt)
	}

	events := recorder.snapshot()
	encodedEvents, _ := json.Marshal(events)
	for _, secret := range []string{"abcdefghijklmnopqrstuvwxyz123456", `{"summary":1`} {
		if strings.Contains(string(encodedEvents), secret) {
			t.Fatalf("activity event leaked protected content %q: %s", secret, encodedEvents)
		}
	}
	for _, eventType := range []string{
		"llm.prompt_redacted",
		"llm.output_validation_failed",
		"llm.output_repair_started",
		"llm.output_repair_completed",
	} {
		if !containsEvent(events, eventType) {
			t.Fatalf("expected event %s in %#v", eventType, events)
		}
	}
}

func TestSafetyGatewayRejectsOversizedResponse(t *testing.T) {
	fake, _ := NewFakeProvider("fake", FakeResult{Response: Response{Content: strings.Repeat("x", 128)}})
	registry, _ := NewRegistry(fake)
	inner, _ := NewGateway(registry, nil)
	gateway, err := NewSafetyGateway(
		inner,
		nil,
		SafetyPolicy{RedactionMode: RedactionModeStrict, MaxStructuredResponseBytes: 32},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := validRequest()
	request.ResponseSchema = validationTestSchema
	_, err = gateway.Complete(context.Background(), "fake", request)
	assertProviderCode(t, err, ErrorStructuredOutputTooLarge)
}

func TestSafetyGatewayCancellationStopsRepair(t *testing.T) {
	fake, _ := NewFakeProvider("fake",
		FakeResult{Response: Response{Content: `{"summary":1,"steps":[]}`}},
		FakeResult{Response: Response{Content: `{"summary":"ok","steps":["one"]}`}, Delay: time.Second},
	)
	registry, _ := NewRegistry(fake)
	inner, _ := NewGateway(registry, nil)
	gateway, err := NewSafetyGateway(
		inner,
		nil,
		SafetyPolicy{
			RedactionMode:              RedactionModeStrict,
			MaxRepairAttempts:          1,
			MaxStructuredResponseBytes: 4096,
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := validRequest()
	request.ResponseSchema = validationTestSchema
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err = gateway.Complete(ctx, "fake", request)
	providerError, ok := AsProviderError(err)
	if !ok || (providerError.Code != ErrorTimeout && providerError.Code != ErrorCancelled) {
		t.Fatalf("expected timeout or cancellation, got %v", err)
	}
	if len(fake.Requests()) != 2 {
		t.Fatalf("unexpected provider call count: %d", len(fake.Requests()))
	}
}

func TestSafetyGatewayRepairHonorsAggregateBudget(t *testing.T) {
	fake, _ := NewFakeProvider("fake",
		FakeResult{Response: Response{
			Content: `{"summary":1,"steps":[]}`,
			Usage:   Usage{InputTokens: 40, OutputTokens: 10},
		}},
	)
	registry, _ := NewRegistry(fake)
	inner, err := NewPolicyGateway(registry, nil, ExecutionPolicy{
		MaxAttempts: 1,
		Budget:      TokenBudget{MaxTotalTokens: 60},
	}, fixedEstimator(5))
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := NewSafetyGateway(
		inner,
		nil,
		SafetyPolicy{
			RedactionMode:              RedactionModeStrict,
			MaxRepairAttempts:          1,
			MaxStructuredResponseBytes: 4096,
		},
		nil,
		nil,
		fixedEstimator(20),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := validRequest()
	request.ResponseSchema = validationTestSchema
	request.MaxOutputTokens = 10
	_, err = gateway.Complete(context.Background(), "fake", request)
	assertProviderCode(t, err, ErrorBudgetExceeded)
	if len(fake.Requests()) != 1 {
		t.Fatalf("repair should have been rejected before provider call, got %d calls", len(fake.Requests()))
	}
}

func containsEvent(events []recordedEvent, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
