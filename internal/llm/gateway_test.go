package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordedEvent struct {
	Type    string
	Payload map[string]any
}

type eventRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (r *eventRecorder) Record(_ context.Context, _ string, eventType string, payload any, _ time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var cloned map[string]any
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return err
	}
	r.mu.Lock()
	r.events = append(r.events, recordedEvent{Type: eventType, Payload: cloned})
	r.mu.Unlock()
	return nil
}

func (r *eventRecorder) snapshot() []recordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedEvent(nil), r.events...)
}

func validRequest() Request {
	return Request{
		Model: "fake-planner-v1",
		Messages: []Message{
			{Role: RoleSystem, Content: "Return JSON."},
			{Role: RoleUser, Content: "secret requirement text"},
		},
		ResponseSchema:  json.RawMessage(`{"type":"object"}`),
		MaxOutputTokens: 512,
		Temperature:     0.1,
		Metadata: map[string]string{
			"plan_id":             "plan-1",
			"plan_version":        "3",
			"planning_context_id": "context-1",
			"api_key":             "must-not-be-recorded",
		},
	}
}

func TestRegistryRejectsDuplicatesAndSortsNames(t *testing.T) {
	alpha, err := NewFakeProvider("Alpha")
	if err != nil {
		t.Fatal(err)
	}
	zeta, err := NewFakeProvider("zeta")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(zeta, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(registry.Names(), ","); got != "alpha,zeta" {
		t.Fatalf("unexpected provider names: %s", got)
	}
	if err := registry.Register(alpha); !errors.Is(err, ErrProviderExists) {
		t.Fatalf("expected duplicate provider error, got %v", err)
	}
	if _, err := registry.Provider(" ALPHA "); err != nil {
		t.Fatalf("normalized provider lookup failed: %v", err)
	}
}

func TestGatewayCompletesAndRedactsActivityPayload(t *testing.T) {
	fake, err := NewFakeProvider("fake", FakeResult{Response: Response{
		ID:           "response-1",
		Content:      `{"summary":"done"}`,
		FinishReason: FinishReasonStop,
		Usage: Usage{
			InputTokens:       120,
			OutputTokens:      40,
			CachedInputTokens: 20,
			TotalTokens:       999,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(fake)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &eventRecorder{}
	gateway, err := NewGateway(registry, recorder)
	if err != nil {
		t.Fatal(err)
	}

	request := validRequest()
	response, err := gateway.Complete(context.Background(), "FAKE", request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != request.Model || response.Usage.TotalTokens != 160 {
		t.Fatalf("unexpected normalized response: %#v", response)
	}

	requests := fake.Requests()
	if len(requests) != 1 || requests[0].Metadata["plan_id"] != "plan-1" {
		t.Fatalf("unexpected captured requests: %#v", requests)
	}
	request.Messages[0].Content = "mutated"
	request.Metadata["plan_id"] = "mutated"
	if requests[0].Messages[0].Content == "mutated" || requests[0].Metadata["plan_id"] == "mutated" {
		t.Fatal("fake provider did not clone the captured request")
	}

	events := recorder.snapshot()
	if len(events) != 2 || events[0].Type != "llm.request_started" || events[1].Type != "llm.request_completed" {
		t.Fatalf("unexpected event sequence: %#v", events)
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret requirement text", "must-not-be-recorded", `{"summary":"done"}`} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("activity payload leaked protected content %q: %s", secret, encoded)
		}
	}
	if events[1].Payload["plan_id"] != "plan-1" || events[1].Payload["plan_version"] != float64(3) {
		t.Fatalf("expected safe correlation metadata, got %#v", events[1].Payload)
	}
}

func TestGatewayReturnsTypedUnknownProviderAndValidationErrors(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := NewGateway(registry, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = gateway.Complete(context.Background(), "missing", validRequest())
	assertProviderCode(t, err, ErrorUnavailable)

	fake, err := NewFakeProvider("fake")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(fake); err != nil {
		t.Fatal(err)
	}
	request := validRequest()
	request.Model = ""
	_, err = gateway.Complete(context.Background(), "fake", request)
	assertProviderCode(t, err, ErrorInvalidRequest)

	request = validRequest()
	request.ResponseSchema = json.RawMessage(`{"type":`)
	_, err = gateway.Complete(context.Background(), "fake", request)
	assertProviderCode(t, err, ErrorInvalidRequest)
}

func TestGatewayNormalizesRateLimitCancellationAndTimeout(t *testing.T) {
	fake, err := NewFakeProvider("fake",
		FakeResult{Err: NewFakeRateLimitError("fake", 2*time.Second)},
		FakeResult{Response: Response{Content: "cancelled"}, Delay: time.Second},
		FakeResult{Response: Response{Content: "timeout"}, Delay: time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(fake)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &eventRecorder{}
	gateway, err := NewGateway(registry, recorder)
	if err != nil {
		t.Fatal(err)
	}

	_, err = gateway.Complete(context.Background(), "fake", validRequest())
	providerError := assertProviderCode(t, err, ErrorRateLimited)
	if !providerError.Retryable || providerError.RetryAfter != 2*time.Second {
		t.Fatalf("unexpected rate limit metadata: %#v", providerError)
	}

	cancelContext, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = gateway.Complete(cancelContext, "fake", validRequest())
	assertProviderCode(t, err, ErrorCancelled)

	timeoutContext, cancelTimeout := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancelTimeout()
	_, err = gateway.Complete(timeoutContext, "fake", validRequest())
	providerError = assertProviderCode(t, err, ErrorTimeout)
	if !providerError.Retryable {
		t.Fatal("timeout should be retryable")
	}

	events := recorder.snapshot()
	var failed, cancelled bool
	for _, event := range events {
		failed = failed || event.Type == "llm.request_failed"
		cancelled = cancelled || event.Type == "llm.request_cancelled"
	}
	if !failed || !cancelled {
		t.Fatalf("expected failed and cancelled events, got %#v", events)
	}
}

func TestFakeProviderReturnsSequentialResults(t *testing.T) {
	fake, err := NewFakeProvider("fake")
	if err != nil {
		t.Fatal(err)
	}
	fake.EnqueueResponse(Response{ID: "one", Content: "first"})
	fake.EnqueueError(fmt.Errorf("second failed"))
	fake.EnqueueResponse(Response{ID: "three", Content: "third"})

	first, err := fake.Complete(context.Background(), validRequest())
	if err != nil || first.ID != "one" {
		t.Fatalf("unexpected first result: %#v, %v", first, err)
	}
	if _, err := fake.Complete(context.Background(), validRequest()); err == nil {
		t.Fatal("expected queued fake error")
	}
	third, err := fake.Complete(context.Background(), validRequest())
	if err != nil || third.ID != "three" {
		t.Fatalf("unexpected third result: %#v, %v", third, err)
	}
	if fake.Pending() != 0 || len(fake.Requests()) != 3 {
		t.Fatalf("unexpected fake state: pending=%d requests=%d", fake.Pending(), len(fake.Requests()))
	}
}

func TestGatewayIsSafeForConcurrentCompletion(t *testing.T) {
	const count = 24
	fake, err := NewFakeProvider("fake")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < count; index++ {
		fake.EnqueueResponse(Response{ID: fmt.Sprintf("response-%d", index), Content: "{}"})
	}
	registry, err := NewRegistry(fake)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := NewGateway(registry, &eventRecorder{})
	if err != nil {
		t.Fatal(err)
	}

	var waitGroup sync.WaitGroup
	errorsChannel := make(chan error, count)
	for index := 0; index < count; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := gateway.Complete(context.Background(), "fake", validRequest())
			errorsChannel <- err
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent completion failed: %v", err)
		}
	}
	if len(fake.Requests()) != count {
		t.Fatalf("expected %d requests, got %d", count, len(fake.Requests()))
	}
}

func assertProviderCode(t *testing.T, err error, expected ErrorCode) *ProviderError {
	t.Helper()
	providerError, ok := AsProviderError(err)
	if !ok {
		t.Fatalf("expected provider error %s, got %T: %v", expected, err, err)
	}
	if providerError.Code != expected {
		t.Fatalf("expected provider error %s, got %s: %v", expected, providerError.Code, err)
	}
	return providerError
}
