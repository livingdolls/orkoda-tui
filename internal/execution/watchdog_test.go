package execution

import (
	"context"
	"testing"
	"time"
)

func TestExecutorProgressWatchdogCancelsStalledStage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	watchdog := newExecutorProgressWatchdog()
	watchdog.Mark("waiting for model response")
	done := watchdog.Start(ctx, cancel, 30*time.Millisecond)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not stop")
	}
	err := watchdog.Failure()
	code, message, paused := classifyExecutorError(err)
	if code != ExecutorStalledCode || paused || message == "" {
		t.Fatalf("classification = %q %q paused=%v", code, message, paused)
	}
}

func TestExecutorProgressWatchdogResetsOnProgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	watchdog := newExecutorProgressWatchdog()
	done := watchdog.Start(ctx, cancel, 80*time.Millisecond)
	for range 3 {
		time.Sleep(30 * time.Millisecond)
		watchdog.Mark("tool completed")
	}
	if err := watchdog.Failure(); err != nil {
		t.Fatalf("watchdog failed during progress: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not stop after cancellation")
	}
}
