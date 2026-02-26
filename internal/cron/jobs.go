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
	PruneByAgent(agentID string, maxIdle time.Duration) int
}

// SessionCleanupJob removes sessions that have been idle longer than MaxIdle.
type SessionCleanupJob struct {
	Store        SessionStore
	MaxIdle      time.Duration
	Logger       *slog.Logger
	AgentID      string // empty = global (all agents)
	ScheduleExpr string // empty = default "*/5 * * * *"
}

// Compile-time interface check.
var _ Job = (*SessionCleanupJob)(nil)

// Name implements Job.
func (j *SessionCleanupJob) Name() string {
	if j.AgentID != "" {
		return "session_cleanup:" + j.AgentID
	}
	return "session_cleanup"
}

// Schedule implements Job.
func (j *SessionCleanupJob) Schedule() string {
	if j.ScheduleExpr != "" {
		return j.ScheduleExpr
	}
	return "*/5 * * * *"
}

// Run prunes sessions idle longer than MaxIdle.
func (j *SessionCleanupJob) Run(_ context.Context) error {
	var pruned int
	if j.AgentID != "" {
		pruned = j.Store.PruneByAgent(j.AgentID, j.MaxIdle)
	} else {
		pruned = j.Store.Prune(j.MaxIdle)
	}
	if pruned > 0 {
		j.Logger.Info("cron: pruned idle sessions", "count", pruned, "agent", j.AgentID)
	}
	return nil
}

// MemoryExtractionJob extracts facts from recent session exchanges.
// This is a stub — the full implementation requires iterating over sessions
// and their history, which will be wired when the memory subsystem is
// integrated with the router's session store.
type MemoryExtractionJob struct {
	Logger       *slog.Logger
	AgentID      string // empty = global
	ScheduleExpr string // empty = default "*/10 * * * *"
}

// Compile-time interface check.
var _ Job = (*MemoryExtractionJob)(nil)

// Name implements Job.
func (j *MemoryExtractionJob) Name() string {
	if j.AgentID != "" {
		return "memory_extraction:" + j.AgentID
	}
	return "memory_extraction"
}

// Schedule implements Job.
func (j *MemoryExtractionJob) Schedule() string {
	if j.ScheduleExpr != "" {
		return j.ScheduleExpr
	}
	return "*/10 * * * *"
}

// Run is a no-op stub until the memory subsystem is wired.
func (j *MemoryExtractionJob) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("cron: memory extraction cancelled: %w", ctx.Err())
	}
	j.Logger.Debug("cron: memory extraction tick (no-op until wired)", "agent", j.AgentID)
	return nil
}

// MemoryCompactionJob compacts long session histories.
// This is a stub — the full implementation requires iterating over sessions
// and invoking the context engine compactor.
type MemoryCompactionJob struct {
	Logger       *slog.Logger
	AgentID      string // empty = global
	ScheduleExpr string // empty = default "0 * * * *"
}

// Compile-time interface check.
var _ Job = (*MemoryCompactionJob)(nil)

// Name implements Job.
func (j *MemoryCompactionJob) Name() string {
	if j.AgentID != "" {
		return "memory_compaction:" + j.AgentID
	}
	return "memory_compaction"
}

// Schedule implements Job.
func (j *MemoryCompactionJob) Schedule() string {
	if j.ScheduleExpr != "" {
		return j.ScheduleExpr
	}
	return "0 * * * *"
}

// Run is a no-op stub until the context engine compactor is wired.
func (j *MemoryCompactionJob) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("cron: memory compaction cancelled: %w", ctx.Err())
	}
	j.Logger.Debug("cron: memory compaction tick (no-op until wired)", "agent", j.AgentID)
	return nil
}
