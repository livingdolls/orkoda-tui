package scheduler

import "time"

func DefaultConfig(workerID string) Config {
	return Config{
		WorkerID:      workerID,
		PollInterval:  250 * time.Millisecond,
		StaleAfter:    5 * time.Minute,
		RetryBase:     time.Second,
		MaxRetryDelay: time.Minute,
	}
}
