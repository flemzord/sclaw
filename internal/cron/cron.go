// Package cron provides a job scheduler for periodic background tasks
// such as session cleanup, memory extraction, and history compaction.
package cron

import "context"

// Job defines a periodic background task.
type Job interface {
	// Name returns a unique identifier for this job (used for logging and dedup).
	Name() string

	// Schedule returns a 5-field cron expression (e.g., "*/5 * * * *").
	Schedule() string

	// Run executes the job. Implementations should check ctx.Done() for
	// graceful cancellation.
	Run(ctx context.Context) error
}
