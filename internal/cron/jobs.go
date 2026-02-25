package cron

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// SessionStore is the subset of router.SessionStore needed by cron jobs.
// Defined here to avoid a circular dependency on the router package.
type SessionStore interface {
	Prune(maxIdle time.Duration) int
}

// SessionCleanupJob removes sessions that have been idle longer than MaxIdle.
type SessionCleanupJob struct {
	Store   SessionStore
	MaxIdle time.Duration
	Logger  *slog.Logger
}

// Compile-time interface check.
var _ Job = (*SessionCleanupJob)(nil)

// Name implements Job.
func (j *SessionCleanupJob) Name() string { return "session_cleanup" }

// Schedule implements Job.
func (j *SessionCleanupJob) Schedule() string { return "*/5 * * * *" }

// Run prunes sessions idle longer than MaxIdle.
func (j *SessionCleanupJob) Run(_ context.Context) error {
	pruned := j.Store.Prune(j.MaxIdle)
	if pruned > 0 {
		j.Logger.Info("cron: pruned idle sessions", "count", pruned)
	}
	return nil
}

// MemoryExtractionJob extracts facts from recent session exchanges.
// This is a stub — the full implementation requires iterating over sessions
// and their history, which will be wired when the memory subsystem is
// integrated with the router's session store.
type MemoryExtractionJob struct {
	Logger *slog.Logger
}

// Compile-time interface check.
var _ Job = (*MemoryExtractionJob)(nil)

// Name implements Job.
func (j *MemoryExtractionJob) Name() string { return "memory_extraction" }

// Schedule implements Job.
func (j *MemoryExtractionJob) Schedule() string { return "*/10 * * * *" }

// Run is a no-op stub until the memory subsystem is wired.
func (j *MemoryExtractionJob) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("cron: memory extraction cancelled: %w", ctx.Err())
	}
	j.Logger.Debug("cron: memory extraction tick (no-op until wired)")
	return nil
}

// MemoryCompactionJob compacts long session histories.
// This is a stub — the full implementation requires iterating over sessions
// and invoking the context engine compactor.
type MemoryCompactionJob struct {
	Logger *slog.Logger
}

// Compile-time interface check.
var _ Job = (*MemoryCompactionJob)(nil)

// Name implements Job.
func (j *MemoryCompactionJob) Name() string { return "memory_compaction" }

// Schedule implements Job.
func (j *MemoryCompactionJob) Schedule() string { return "0 * * * *" }

// Run is a no-op stub until the context engine compactor is wired.
func (j *MemoryCompactionJob) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("cron: memory compaction cancelled: %w", ctx.Err())
	}
	j.Logger.Debug("cron: memory compaction tick (no-op until wired)")
	return nil
}
