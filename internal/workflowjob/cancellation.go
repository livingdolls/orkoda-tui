package workflowjob

import (
	"context"
	"fmt"
	"time"
)

// StartCancellationWatcher bridges the durable cancellation flag to an
// in-process context. Every active stage uses it so a cancel request stops
// model calls, sandbox commands, and lease renewal instead of only changing
// the database row.
func StartCancellationWatcher(
	ctx context.Context,
	workflowID string,
	interval time.Duration,
	get func(context.Context, string) (Job, error),
	cancel context.CancelFunc,
	result chan<- error,
) <-chan struct{} {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				job, err := get(context.WithoutCancel(ctx), workflowID)
				if err != nil {
					select {
					case result <- fmt.Errorf("poll workflow cancellation: %w", err):
					default:
					}
					cancel()
					return
				}
				if job.CancellationRequested || job.Status == StatusCancelled {
					select {
					case result <- context.Canceled:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()
	return done
}

// WithWallClock applies the aggregate's wall-clock budget to an active stage.
// Test doubles and legacy rows without a creation timestamp retain the parent
// context rather than receiving a year-1 deadline.
func WithWallClock(ctx context.Context, job Job) (context.Context, context.CancelFunc) {
	if job.CreatedAt.IsZero() || job.Limits.WallClockSeconds <= 0 {
		return context.WithCancel(ctx)
	}
	deadline := job.CreatedAt.Add(time.Duration(job.Limits.WallClockSeconds) * time.Second)
	return context.WithDeadline(ctx, deadline)
}
