package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
)

type fakeQueue struct {
	claimJobs       []*jobqueue.Job
	claimErr        error
	completeIDs     []string
	failCalls       []failCall
	recoveredBefore time.Time
	recoveredAt     time.Time
	recoveredIDs    []string
}

type failCall struct {
	id      string
	failure string
	retryAt time.Time
	now     time.Time
}

type recordCall struct {
	jobID     string
	eventType string
	payload   any
	createdAt time.Time
}

type fakeActivityRecorder struct {
	calls    []recordCall
	err      error
	onRecord func(recordCall)
}

func (f *fakeQueue) Claim(context.Context, string, time.Time) (*jobqueue.Job, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if len(f.claimJobs) == 0 {
		return nil, nil
	}

	job := f.claimJobs[0]
	f.claimJobs = f.claimJobs[1:]
	return job, nil
}

func (f *fakeQueue) Complete(_ context.Context, id string, _ time.Time) error {
	f.completeIDs = append(f.completeIDs, id)
	return nil
}

func (f *fakeQueue) Fail(_ context.Context, id, failure string, retryAt, now time.Time) (string, error) {
	f.failCalls = append(f.failCalls, failCall{
		id:      id,
		failure: failure,
		retryAt: retryAt,
		now:     now,
	})
	if len(f.claimJobs) == 0 && failure == "permanent" {
		return "DEAD", nil
	}
	return "QUEUED", nil
}

func (f *fakeQueue) RecoverStale(_ context.Context, before, now time.Time) ([]string, error) {
	f.recoveredBefore = before
	f.recoveredAt = now
	return append([]string(nil), f.recoveredIDs...), nil
}

func (f *fakeActivityRecorder) Record(_ context.Context, jobID, eventType string, payload any, createdAt time.Time) error {
	if f.err != nil {
		return f.err
	}
	call := recordCall{jobID: jobID, eventType: eventType, payload: payload, createdAt: createdAt}
	f.calls = append(f.calls, call)
	if f.onRecord != nil {
		f.onRecord(call)
	}
	return nil
}

func testConfig() Config {
	return Config{
		WorkerID:      "test-worker",
		PollInterval:  time.Millisecond,
		StaleAfter:    time.Minute,
		RetryBase:     time.Second,
		MaxRetryDelay: time.Minute,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunCompletesSuccessfulJobAndRecordsActivity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queue := &fakeQueue{claimJobs: []*jobqueue.Job{{
		ID: "job-1", Type: "test", Attempts: 1, MaxAttempts: 3,
	}}}
	activities := &fakeActivityRecorder{}
	activities.onRecord = func(call recordCall) {
		if call.eventType == "job.completed" {
			cancel()
		}
	}

	scheduler, err := New(queue, testConfig(), map[string]Handler{
		"test": func(context.Context, jobqueue.Job) error { return nil },
	}, activities, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(queue.completeIDs) != 1 || queue.completeIDs[0] != "job-1" {
		t.Fatalf("complete IDs = %#v", queue.completeIDs)
	}
	assertEventTypes(t, activities.calls, "job.started", "job.completed")
}

func TestRunRetriesFailedJobWithExponentialBackoff(t *testing.T) {
	fixedNow := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	queue := &fakeQueue{claimJobs: []*jobqueue.Job{{
		ID: "job-2", Type: "test", Attempts: 3, MaxAttempts: 5,
	}}}
	activities := &fakeActivityRecorder{}
	activities.onRecord = func(call recordCall) {
		if call.eventType == "job.retry_scheduled" {
			cancel()
		}
	}

	scheduler, err := New(queue, testConfig(), map[string]Handler{
		"test": func(context.Context, jobqueue.Job) error { return errors.New("temporary") },
	}, activities, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	scheduler.now = func() time.Time { return fixedNow }

	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(queue.failCalls) != 1 {
		t.Fatalf("fail calls = %d", len(queue.failCalls))
	}
	if got, want := queue.failCalls[0].retryAt, fixedNow.Add(4*time.Second); !got.Equal(want) {
		t.Fatalf("retryAt = %v, want %v", got, want)
	}
	assertEventTypes(t, activities.calls, "job.started", "job.retry_scheduled")
}

func TestRunRecordsDeadJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queue := &fakeQueue{claimJobs: []*jobqueue.Job{{
		ID: "job-dead", Type: "test", Attempts: 1, MaxAttempts: 1,
	}}}
	activities := &fakeActivityRecorder{}
	activities.onRecord = func(call recordCall) {
		if call.eventType == "job.dead" {
			cancel()
		}
	}

	scheduler, err := New(queue, testConfig(), map[string]Handler{
		"test": func(context.Context, jobqueue.Job) error { return errors.New("permanent") },
	}, activities, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertEventTypes(t, activities.calls, "job.started", "job.dead")
}

func TestRunRecoversStaleJobsOnStartup(t *testing.T) {
	fixedNow := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	queue := &fakeQueue{recoveredIDs: []string{"job-a", "job-b"}}
	activities := &fakeActivityRecorder{}
	activities.onRecord = func(call recordCall) {
		if call.jobID == "job-b" {
			cancel()
		}
	}

	scheduler, err := New(queue, testConfig(), nil, activities, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	scheduler.now = func() time.Time { return fixedNow }

	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := queue.recoveredBefore, fixedNow.Add(-time.Minute); !got.Equal(want) {
		t.Fatalf("lockedBefore = %v, want %v", got, want)
	}
	assertEventTypes(t, activities.calls, "job.recovered", "job.recovered")
}

func TestRunFailsUnknownJobThroughQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queue := &fakeQueue{claimJobs: []*jobqueue.Job{{
		ID: "job-3", Type: "unknown", Attempts: 1, MaxAttempts: 3,
	}}}
	activities := &fakeActivityRecorder{}
	activities.onRecord = func(call recordCall) {
		if call.eventType == "job.retry_scheduled" {
			cancel()
		}
	}

	scheduler, err := New(queue, testConfig(), nil, activities, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(queue.failCalls) != 1 {
		t.Fatalf("fail calls = %d", len(queue.failCalls))
	}
}

func TestRunReturnsClaimFailure(t *testing.T) {
	queue := &fakeQueue{claimErr: errors.New("database unavailable")}
	scheduler, err := New(queue, testConfig(), nil, nil, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := scheduler.Run(context.Background()); err == nil {
		t.Fatal("Run() expected an error")
	}
}

func TestRunReturnsActivityFailureWithoutPublishingProgress(t *testing.T) {
	queue := &fakeQueue{claimJobs: []*jobqueue.Job{{
		ID: "job-4", Type: "test", Attempts: 1, MaxAttempts: 3,
	}}}
	activities := &fakeActivityRecorder{err: errors.New("database unavailable")}
	scheduler, err := New(queue, testConfig(), nil, activities, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := scheduler.Run(context.Background()); err == nil {
		t.Fatal("Run() expected an error")
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	queue := &fakeQueue{}
	config := testConfig()
	config.MaxRetryDelay = config.RetryBase / 2

	if _, err := New(queue, config, nil, nil, testLogger()); err == nil {
		t.Fatal("New() expected an error")
	}
}

func assertEventTypes(t *testing.T, calls []recordCall, expected ...string) {
	t.Helper()
	if len(calls) != len(expected) {
		t.Fatalf("recorded events = %#v, want %v", calls, expected)
	}
	for index, eventType := range expected {
		if calls[index].eventType != eventType {
			t.Fatalf("event %d type = %q, want %q", index, calls[index].eventType, eventType)
		}
	}
}
