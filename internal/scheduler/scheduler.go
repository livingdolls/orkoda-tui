package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
)

type Queue interface {
	Claim(context.Context, string, time.Time) (*jobqueue.Job, error)
	Complete(context.Context, string, time.Time) error
	Fail(context.Context, string, string, time.Time, time.Time) (string, error)
	RecoverStale(context.Context, time.Time, time.Time) (int64, error)
}

type Handler func(context.Context, jobqueue.Job) error

type Config struct {
	WorkerID      string
	PollInterval  time.Duration
	StaleAfter    time.Duration
	RetryBase     time.Duration
	MaxRetryDelay time.Duration
}

type Scheduler struct {
	queue    Queue
	config   Config
	handlers map[string]Handler
	logger   *slog.Logger
	now      func() time.Time
}

func New(queue Queue, config Config, handlers map[string]Handler, logger *slog.Logger) (*Scheduler, error) {
	if queue == nil {
		return nil, fmt.Errorf("queue is required")
	}
	if config.WorkerID == "" {
		return nil, fmt.Errorf("worker ID is required")
	}
	if config.PollInterval <= 0 {
		return nil, fmt.Errorf("poll interval must be greater than zero")
	}
	if config.StaleAfter <= 0 {
		return nil, fmt.Errorf("stale duration must be greater than zero")
	}
	if config.RetryBase <= 0 {
		return nil, fmt.Errorf("retry base must be greater than zero")
	}
	if config.MaxRetryDelay < config.RetryBase {
		return nil, fmt.Errorf("maximum retry delay must be at least the retry base")
	}
	if logger == nil {
		logger = slog.Default()
	}

	registered := make(map[string]Handler, len(handlers))
	for jobType, handler := range handlers {
		if jobType != "" && handler != nil {
			registered[jobType] = handler
		}
	}

	return &Scheduler{
		queue:    queue,
		config:   config,
		handlers: registered,
		logger:   logger,
		now:      time.Now,
	}, nil
}

func (s *Scheduler) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}

	now := s.now().UTC()
	recovered, err := s.queue.RecoverStale(ctx, now.Add(-s.config.StaleAfter), now)
	if err != nil {
		return fmt.Errorf("recover stale jobs: %w", err)
	}
	if recovered > 0 {
		s.logger.Info("recovered stale jobs", "count", recovered)
	}

	for {
		if ctx.Err() != nil {
			return nil
		}

		now = s.now().UTC()
		job, err := s.queue.Claim(ctx, s.config.WorkerID, now)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("claim job: %w", err)
		}
		if job == nil {
			if !wait(ctx, s.config.PollInterval) {
				return nil
			}
			continue
		}

		if err := s.process(ctx, *job); err != nil {
			return err
		}
	}
}

func (s *Scheduler) process(ctx context.Context, job jobqueue.Job) error {
	handler, ok := s.handlers[job.Type]
	var handlerErr error
	if !ok {
		handlerErr = fmt.Errorf("no handler registered for job type %q", job.Type)
	} else {
		handlerErr = handler(ctx, job)
	}

	// Leave an interrupted job in RUNNING state. The next daemon startup will
	// recover it after the stale threshold rather than consuming an attempt.
	if ctx.Err() != nil {
		return nil
	}

	now := s.now().UTC()
	if handlerErr == nil {
		if err := s.queue.Complete(ctx, job.ID, now); err != nil {
			return fmt.Errorf("complete job %s: %w", job.ID, err)
		}
		s.logger.Info("job completed", "job_id", job.ID, "job_type", job.Type, "attempt", job.Attempts)
		return nil
	}

	retryAt := now.Add(s.retryDelay(job.Attempts))
	status, err := s.queue.Fail(ctx, job.ID, handlerErr.Error(), retryAt, now)
	if err != nil {
		return fmt.Errorf("fail job %s: %w", job.ID, err)
	}
	s.logger.Warn(
		"job failed",
		"job_id", job.ID,
		"job_type", job.Type,
		"attempt", job.Attempts,
		"status", status,
		"retry_at", retryAt,
		"error", handlerErr,
	)
	return nil
}

func (s *Scheduler) retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := s.config.RetryBase
	for current := 1; current < attempt; current++ {
		if delay >= s.config.MaxRetryDelay/2 {
			return s.config.MaxRetryDelay
		}
		delay *= 2
	}
	if delay > s.config.MaxRetryDelay {
		return s.config.MaxRetryDelay
	}
	return delay
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
