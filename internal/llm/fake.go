package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type FakeResult struct {
	Response Response
	Err      error
	Delay    time.Duration
}

type FakeProvider struct {
	name string

	mu       sync.Mutex
	results  []FakeResult
	requests []Request
}

func NewFakeProvider(name string, results ...FakeResult) (*FakeProvider, error) {
	name = normalizeProviderName(name)
	if name == "" {
		return nil, fmt.Errorf("fake provider name is required")
	}
	provider := &FakeProvider{name: name}
	for _, result := range results {
		provider.Enqueue(result)
	}
	return provider, nil
}

func (f *FakeProvider) Name() string {
	if f == nil {
		return ""
	}
	return f.name
}

func (f *FakeProvider) Complete(ctx context.Context, request Request) (Response, error) {
	if f == nil {
		return Response{}, fmt.Errorf("fake provider is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	f.mu.Lock()
	f.requests = append(f.requests, cloneRequest(request))
	if len(f.results) == 0 {
		f.mu.Unlock()
		return Response{}, &ProviderError{
			Provider:  f.name,
			Code:      ErrorUnavailable,
			Message:   "fake provider has no queued result",
			Retryable: false,
		}
	}
	result := f.results[0]
	f.results = append([]FakeResult(nil), f.results[1:]...)
	f.mu.Unlock()

	if result.Delay > 0 {
		timer := time.NewTimer(result.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case <-timer.C:
		}
	} else {
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		default:
		}
	}

	if result.Err != nil {
		return Response{}, result.Err
	}
	return cloneResponse(result.Response), nil
}

func (f *FakeProvider) Enqueue(result FakeResult) {
	if f == nil {
		return
	}
	result.Response = cloneResponse(result.Response)
	f.mu.Lock()
	f.results = append(f.results, result)
	f.mu.Unlock()
}

func (f *FakeProvider) EnqueueResponse(response Response) {
	f.Enqueue(FakeResult{Response: response})
}

func (f *FakeProvider) EnqueueError(err error) {
	f.Enqueue(FakeResult{Err: err})
}

func (f *FakeProvider) EnqueueDelayed(response Response, delay time.Duration) {
	f.Enqueue(FakeResult{Response: response, Delay: delay})
}

func (f *FakeProvider) Requests() []Request {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	requests := make([]Request, 0, len(f.requests))
	for _, request := range f.requests {
		requests = append(requests, cloneRequest(request))
	}
	return requests
}

func (f *FakeProvider) Pending() int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.results)
}

func NewFakeRateLimitError(provider string, retryAfter time.Duration) error {
	return &ProviderError{
		Provider:   strings.TrimSpace(provider),
		Code:       ErrorRateLimited,
		Message:    "rate limit exceeded",
		Retryable:  true,
		RetryAfter: retryAfter,
	}
}
