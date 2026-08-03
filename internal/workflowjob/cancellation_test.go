package workflowjob

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestCancellationWatcherCancelsActiveStage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var cancelled atomic.Bool
	result := make(chan error, 1)
	done := StartCancellationWatcher(ctx, "workflow-1", 50*time.Millisecond, func(context.Context, string) (Job, error) {
		if cancelled.Load() {
			return Job{Status: StatusCancelled, CancellationRequested: true}, nil
		}
		return Job{Status: StatusExecuting}, nil
	}, cancel, result)
	cancelled.Store(true)
	select {
	case err := <-result:
		if err != context.Canceled {
			t.Fatalf("watcher error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation watcher did not observe cancellation")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation watcher did not stop")
	}
}

func TestWithWallClockUsesAggregateCreationDeadline(t *testing.T) {
	created := time.Now().Add(-2 * time.Second)
	ctx, cancel := WithWallClock(context.Background(), Job{
		CreatedAt: created,
		Limits:    Limits{WallClockSeconds: 1},
	})
	defer cancel()
	if err := ctx.Err(); err == nil || err != context.DeadlineExceeded {
		t.Fatalf("context error = %v, want deadline exceeded", err)
	}
}
