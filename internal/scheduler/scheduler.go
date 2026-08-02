package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
)

const persistenceTimeout = 5 * time.Second

type Queue interface {
	Claim(context.Context, string, time.Time) (*jobqueue.Job, error)
	Complete(context.Context, string, time.Time) error
	Fail(context.Context, string, string, time.Time, time.Time) (string, error)
	RecoverStale(context.Context, time.Time, time.Time) ([]string, error)
}

type typedQueue interface {
	ClaimTypes(context.Context, string, time.Time, []string) (*jobqueue.Job, error)
}

type ActivityRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
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
	queue        Queue
	config       Config
	handlers     map[string]Handler
	handlerTypes []string
	activities   ActivityRecorder
	logger       *slog.Logger
	now          func() time.Time
}

func New(
	queue Queue,
	config Config,
	handlers map[string]Handler,
	activities ActivityRecorder,
	logger *slog.Logger,
) (*Scheduler, error) {
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
	if activities == nil {
		activities = discardActivityRecorder{}
	}

	registered := make(map[string]Handler, len(handlers))
	handlerTypes := make([]string, 0, len(handlers))
	for jobType, handler := range handlers {
		if jobType != "" && handler != nil {
			registered[jobType] = handler
			handlerTypes = append(handlerTypes, jobType)
		}
	}
	sort.Strings(handlerTypes)

	return &Scheduler{
		queue:        queue,
		config:       config,
		handlers:     registered,
		handlerTypes: handlerTypes,
		activities:   activities,
		logger:       logger,
		now:          time.Now,
	}, nil
}

func (s *Scheduler) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}

	now := s.now().UTC()
	recoveredIDs, err := s.queue.RecoverStale(ctx, now.Add(-s.config.StaleAfter), now)
	if err != nil {
		return fmt.Errorf("recover stale jobs: %w", err)
	}
	for _, jobID := range recoveredIDs {
		if err := s.record(ctx, jobID, "job.recovered", map[string]any{
			"worker_id":    s.config.WorkerID,
			"recovered_at": now.Format(time.RFC3339Nano),
		}, now); err != nil {
			return fmt.Errorf("record recovered job %s: %w", jobID, err)
		}
	}
	if len(recoveredIDs) > 0 {
		s.logger.Info("recovered stale jobs", "count", len(recoveredIDs))
	}

	for {
		if ctx.Err() != nil {
			return nil
		}

		now = s.now().UTC()
		job, err := s.claim(ctx, now)
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

		if err := s.record(ctx, job.ID, "job.started", map[string]any{
			"job_type":     job.Type,
			"attempt":      job.Attempts,
			"max_attempts": job.MaxAttempts,
			"worker_id":    s.config.WorkerID,
		}, now); err != nil {
			return fmt.Errorf("record started job %s: %w", job.ID, err)
		}

		if err := s.process(ctx, *job); err != nil {
			return err
		}
	}
}

func (s *Scheduler) claim(ctx context.Context, now time.Time) (*jobqueue.Job, error) {
	if queue, ok := s.queue.(typedQueue); ok {
		return queue.ClaimTypes(ctx, s.config.WorkerID, now, s.handlerTypes)
	}
	return s.queue.Claim(ctx, s.config.WorkerID, now)
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
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistenceTimeout)
	defer cancel()

	if handlerErr == nil {
		if err := s.queue.Complete(persistCtx, job.ID, now); err != nil {
			return fmt.Errorf("complete job %s: %w", job.ID, err)
		}
		if err := s.activities.Record(persistCtx, job.ID, "job.completed", map[string]any{
			"job_type": job.Type,
			"attempt":  job.Attempts,
		}, now); err != nil {
			return fmt.Errorf("record completed job %s: %w", job.ID, err)
		}
		s.logger.Info("job completed", "job_id", job.ID, "job_type", job.Type, "attempt", job.Attempts)
		return nil
	}

	retryAt := now.Add(s.retryDelay(job.Attempts))
	status, err := s.queue.Fail(persistCtx, job.ID, handlerErr.Error(), retryAt, now)
	if err != nil {
		return fmt.Errorf("fail job %s: %w", job.ID, err)
	}

	eventType := "job.retry_scheduled"
	payload := map[string]any{
		"job_type":     job.Type,
		"attempt":      job.Attempts,
		"max_attempts": job.MaxAttempts,
		"error":        handlerErr.Error(),
	}
	if status == "DEAD" {
		eventType = "job.dead"
	} else {
		payload["retry_at"] = retryAt.Format(time.RFC3339Nano)
	}
	if err := s.activities.Record(persistCtx, job.ID, eventType, payload, now); err != nil {
		return fmt.Errorf("record failed job %s: %w", job.ID, err)
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

func (s *Scheduler) record(ctx context.Context, jobID, eventType string, payload any, createdAt time.Time) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistenceTimeout)
	defer cancel()
	return s.activities.Record(persistCtx, jobID, eventType, payload, createdAt)
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

type discardActivityRecorder struct{}

func (discardActivityRecorder) Record(context.Context, string, string, any, time.Time) error {
	return nil
}
