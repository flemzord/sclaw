package cron

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/provider"
)

// SessionStore is the subset of router.SessionStore needed by cron jobs.
// Defined here to avoid a circular dependency on the router package.
type SessionStore interface {
	Prune(maxIdle time.Duration) int
	PruneByAgent(agentID string, maxIdle time.Duration) int
}

// SessionRanger iterates over active sessions. Defined here to avoid
// importing the router package (which would create a circular dependency).
type SessionRanger interface {
	Range(fn func(sessionID, agentID string) bool)
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
// When Sessions, History, Store, and Extractor are all non-nil, the job
// iterates active sessions, pairs new user+assistant messages into Exchanges,
// extracts facts via the Extractor, and indexes them into the Store.
// If any dependency is nil the job gracefully no-ops.
type MemoryExtractionJob struct {
	Logger       *slog.Logger
	AgentID      string // empty = global
	ScheduleExpr string // empty = default "*/10 * * * *"

	Sessions  SessionRanger
	History   memory.HistoryStore
	Store     memory.Store
	Extractor memory.FactExtractor

	// lastLen tracks how many messages have been processed per session
	// so that only new messages are extracted on each tick.
	lastLen map[string]int
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

// Run extracts facts from new session messages since the last tick.
// Gracefully no-ops when any required dependency is nil.
func (j *MemoryExtractionJob) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("cron: memory extraction cancelled: %w", ctx.Err())
	}

	// Graceful no-op when dependencies are not wired.
	if j.Sessions == nil || j.History == nil || j.Store == nil || j.Extractor == nil {
		j.Logger.Debug("cron: memory extraction skipped (deps not wired)", "agent", j.AgentID)
		return nil
	}

	if j.lastLen == nil {
		j.lastLen = make(map[string]int)
	}

	// Collect matching sessions.
	type sessionEntry struct {
		id      string
		agentID string
	}
	var sessions []sessionEntry
	j.Sessions.Range(func(sessionID, agentID string) bool {
		if j.AgentID == "" || agentID == j.AgentID {
			sessions = append(sessions, sessionEntry{id: sessionID, agentID: agentID})
		}
		return true
	})

	const maxConsecutiveErrors = 3
	consecutiveErrors := 0

	for _, sess := range sessions {
		if ctx.Err() != nil {
			return fmt.Errorf("cron: memory extraction cancelled: %w", ctx.Err())
		}
		if consecutiveErrors >= maxConsecutiveErrors {
			j.Logger.Warn("cron: memory extraction stopping after consecutive errors",
				"errors", consecutiveErrors, "agent", j.AgentID)
			break
		}

		if err := j.extractSession(ctx, sess.id); err != nil {
			consecutiveErrors++
			j.Logger.Error("cron: memory extraction failed for session",
				"session", sess.id, "error", err, "agent", j.AgentID)
			continue
		}
		consecutiveErrors = 0
	}

	return nil
}

// extractSession processes new messages for a single session.
func (j *MemoryExtractionJob) extractSession(ctx context.Context, sessionID string) error {
	currentLen, err := j.History.Len(sessionID)
	if err != nil {
		return fmt.Errorf("getting history length: %w", err)
	}

	prev := j.lastLen[sessionID]
	if currentLen <= prev {
		return nil // no new messages
	}

	newCount := currentLen - prev
	msgs, err := j.History.GetRecent(sessionID, newCount)
	if err != nil {
		return fmt.Errorf("getting recent messages: %w", err)
	}

	// Pair consecutive user+assistant messages into exchanges.
	var extracted int
	for i := 0; i+1 < len(msgs); i += 2 {
		user, asst := msgs[i], msgs[i+1]
		if user.Role != provider.MessageRoleUser || asst.Role != provider.MessageRoleAssistant {
			continue
		}

		exchange := memory.Exchange{
			SessionID:        sessionID,
			UserMessage:      user,
			AssistantMessage: asst,
		}

		facts, err := j.Extractor.Extract(ctx, exchange)
		if err != nil {
			return fmt.Errorf("extracting facts: %w", err)
		}

		for _, fact := range facts {
			if err := j.Store.Index(ctx, fact); err != nil {
				return fmt.Errorf("indexing fact: %w", err)
			}
			extracted++
		}
	}

	j.lastLen[sessionID] = currentLen

	if extracted > 0 {
		j.Logger.Info("cron: extracted facts from session",
			"session", sessionID, "facts", extracted, "agent", j.AgentID)
	}

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
