package execution

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	defaultExecutorStallTimeout = 3 * time.Minute
	ExecutorStalledCode         = "EXECUTOR_STALLED"
)

type executorProgressWatchdog struct {
	mu           sync.Mutex
	lastProgress time.Time
	phase        string
	failure      error
	now          func() time.Time
}

func newExecutorProgressWatchdog() *executorProgressWatchdog {
	return &executorProgressWatchdog{lastProgress: time.Now().UTC(), phase: "starting", now: time.Now}
}

func (w *executorProgressWatchdog) Mark(phase string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.lastProgress = w.now().UTC()
	if phase = strings.TrimSpace(phase); phase != "" {
		w.phase = phase
	}
	w.mu.Unlock()
}

func (w *executorProgressWatchdog) Start(
	ctx context.Context,
	cancel context.CancelFunc,
	timeout time.Duration,
) <-chan struct{} {
	done := make(chan struct{})
	if timeout <= 0 {
		timeout = defaultExecutorStallTimeout
	}
	interval := timeout / 4
	if interval > 5*time.Second {
		interval = 5 * time.Second
	}
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.mu.Lock()
				idle := w.now().UTC().Sub(w.lastProgress)
				phase := w.phase
				if idle >= timeout && w.failure == nil {
					w.failure = &persistedExecutorFailure{
						code: ExecutorStalledCode,
						message: fmt.Sprintf(
							"Executor made no durable progress for %s while %s.",
							timeout.Round(time.Second),
							phase,
						),
					}
					w.mu.Unlock()
					cancel()
					return
				}
				w.mu.Unlock()
			}
		}
	}()
	return done
}

func (w *executorProgressWatchdog) Failure() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failure
}
