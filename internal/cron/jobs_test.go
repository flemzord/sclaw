package cron

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/provider"
)

// testSessionStore implements SessionStore for job tests.
type testSessionStore struct {
	pruneCalls        atomic.Int32
	pruneByAgentCalls atomic.Int32
	pruneFunc         func(maxIdle time.Duration) int
	pruneByAgentFunc  func(agentID string, maxIdle time.Duration) int
}

func (s *testSessionStore) Prune(maxIdle time.Duration) int {
	s.pruneCalls.Add(1)
	if s.pruneFunc != nil {
		return s.pruneFunc(maxIdle)
	}
	return 0
}

func (s *testSessionStore) PruneByAgent(agentID string, maxIdle time.Duration) int {
	s.pruneByAgentCalls.Add(1)
	if s.pruneByAgentFunc != nil {
		return s.pruneByAgentFunc(agentID, maxIdle)
	}
	return 0
}

func TestSessionCleanupJob_Name(t *testing.T) {
	t.Parallel()
	j := &SessionCleanupJob{Logger: slog.Default()}
	if j.Name() != "session_cleanup" {
		t.Errorf("name = %q, want %q", j.Name(), "session_cleanup")
	}
}

func TestSessionCleanupJob_NameWithAgent(t *testing.T) {
	t.Parallel()
	j := &SessionCleanupJob{Logger: slog.Default(), AgentID: "support"}
	if got := j.Name(); got != "session_cleanup:support" {
		t.Errorf("name = %q, want %q", got, "session_cleanup:support")
	}
}

func TestSessionCleanupJob_Schedule(t *testing.T) {
	t.Parallel()
	j := &SessionCleanupJob{Logger: slog.Default()}
	if j.Schedule() != "*/5 * * * *" {
		t.Errorf("schedule = %q, want %q", j.Schedule(), "*/5 * * * *")
	}
}

func TestSessionCleanupJob_ScheduleOverride(t *testing.T) {
	t.Parallel()
	j := &SessionCleanupJob{Logger: slog.Default(), ScheduleExpr: "*/2 * * * *"}
	if got := j.Schedule(); got != "*/2 * * * *" {
		t.Errorf("schedule = %q, want %q", got, "*/2 * * * *")
	}
}

func TestSessionCleanupJob_Run(t *testing.T) {
	t.Parallel()

	store := &testSessionStore{
		pruneFunc: func(maxIdle time.Duration) int {
			if maxIdle != 30*time.Minute {
				t.Errorf("maxIdle = %v, want 30m", maxIdle)
			}
			return 3
		},
	}

	j := &SessionCleanupJob{
		Store:   store,
		MaxIdle: 30 * time.Minute,
		Logger:  slog.Default(),
	}

	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.pruneCalls.Load() != 1 {
		t.Errorf("prune calls = %d, want 1", store.pruneCalls.Load())
	}
}

func TestSessionCleanupJob_RunByAgent(t *testing.T) {
	t.Parallel()

	store := &testSessionStore{
		pruneByAgentFunc: func(agentID string, maxIdle time.Duration) int {
			if agentID != "support" {
				t.Errorf("agentID = %q, want %q", agentID, "support")
			}
			if maxIdle != time.Hour {
				t.Errorf("maxIdle = %v, want 1h", maxIdle)
			}
			return 2
		},
	}

	j := &SessionCleanupJob{
		Store:   store,
		MaxIdle: time.Hour,
		Logger:  slog.Default(),
		AgentID: "support",
	}

	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.pruneByAgentCalls.Load() != 1 {
		t.Errorf("pruneByAgent calls = %d, want 1", store.pruneByAgentCalls.Load())
	}
	if store.pruneCalls.Load() != 0 {
		t.Errorf("prune calls = %d, want 0 (should use PruneByAgent)", store.pruneCalls.Load())
	}
}

func TestMemoryExtractionJob_Name(t *testing.T) {
	t.Parallel()
	j := &MemoryExtractionJob{Logger: slog.Default()}
	if j.Name() != "memory_extraction" {
		t.Errorf("name = %q, want %q", j.Name(), "memory_extraction")
	}
}

func TestMemoryExtractionJob_NameWithAgent(t *testing.T) {
	t.Parallel()
	j := &MemoryExtractionJob{Logger: slog.Default(), AgentID: "sales"}
	if got := j.Name(); got != "memory_extraction:sales" {
		t.Errorf("name = %q, want %q", got, "memory_extraction:sales")
	}
}

func TestMemoryExtractionJob_Schedule(t *testing.T) {
	t.Parallel()
	j := &MemoryExtractionJob{Logger: slog.Default()}
	if j.Schedule() != "*/10 * * * *" {
		t.Errorf("schedule = %q, want %q", j.Schedule(), "*/10 * * * *")
	}
}

func TestMemoryExtractionJob_ScheduleOverride(t *testing.T) {
	t.Parallel()
	j := &MemoryExtractionJob{Logger: slog.Default(), ScheduleExpr: "*/15 * * * *"}
	if got := j.Schedule(); got != "*/15 * * * *" {
		t.Errorf("schedule = %q, want %q", got, "*/15 * * * *")
	}
}

func TestMemoryExtractionJob_Run(t *testing.T) {
	t.Parallel()
	j := &MemoryExtractionJob{Logger: slog.Default()}
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMemoryExtractionJob_CancelledContext(t *testing.T) {
	t.Parallel()
	j := &MemoryExtractionJob{Logger: slog.Default()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := j.Run(ctx); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestMemoryCompactionJob_Name(t *testing.T) {
	t.Parallel()
	j := &MemoryCompactionJob{Logger: slog.Default()}
	if j.Name() != "memory_compaction" {
		t.Errorf("name = %q, want %q", j.Name(), "memory_compaction")
	}
}

func TestMemoryCompactionJob_NameWithAgent(t *testing.T) {
	t.Parallel()
	j := &MemoryCompactionJob{Logger: slog.Default(), AgentID: "ops"}
	if got := j.Name(); got != "memory_compaction:ops" {
		t.Errorf("name = %q, want %q", got, "memory_compaction:ops")
	}
}

func TestMemoryCompactionJob_Schedule(t *testing.T) {
	t.Parallel()
	j := &MemoryCompactionJob{Logger: slog.Default()}
	if j.Schedule() != "0 * * * *" {
		t.Errorf("schedule = %q, want %q", j.Schedule(), "0 * * * *")
	}
}

func TestMemoryCompactionJob_ScheduleOverride(t *testing.T) {
	t.Parallel()
	j := &MemoryCompactionJob{Logger: slog.Default(), ScheduleExpr: "0 */2 * * *"}
	if got := j.Schedule(); got != "0 */2 * * *" {
		t.Errorf("schedule = %q, want %q", got, "0 */2 * * *")
	}
}

func TestMemoryCompactionJob_Run(t *testing.T) {
	t.Parallel()
	j := &MemoryCompactionJob{Logger: slog.Default()}
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMemoryCompactionJob_CancelledContext(t *testing.T) {
	t.Parallel()
	j := &MemoryCompactionJob{Logger: slog.Default()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := j.Run(ctx); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// --- Test helpers for MemoryExtractionJob ---

// testSessionRanger implements SessionRanger for tests.
type testSessionRanger struct {
	sessions []struct{ id, agentID string }
}

func (r *testSessionRanger) Range(fn func(string, string) bool) {
	for _, s := range r.sessions {
		if !fn(s.id, s.agentID) {
			return
		}
	}
}

// staticExtractor returns pre-configured facts for every exchange.
type staticExtractor struct {
	facts []memory.Fact
	err   error
	calls int
}

func (e *staticExtractor) Extract(_ context.Context, ex memory.Exchange) ([]memory.Fact, error) {
	e.calls++
	if e.err != nil {
		return nil, e.err
	}
	// Use call count to generate unique IDs across invocations.
	result := make([]memory.Fact, len(e.facts))
	for i, f := range e.facts {
		f.Source = ex.SessionID
		f.ID = fmt.Sprintf("%s-%d-%d", ex.SessionID, e.calls, i)
		result[i] = f
	}
	return result, nil
}

func TestMemoryExtractionJob_Run_NilDepsNoop(t *testing.T) {
	t.Parallel()
	j := &MemoryExtractionJob{Logger: slog.Default()}
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMemoryExtractionJob_Run_ExtractsFacts(t *testing.T) {
	t.Parallel()

	history := memory.NewInMemoryHistoryStore()
	_ = history.Append("sess1", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "I like Go"})
	_ = history.Append("sess1", provider.LLMMessage{Role: provider.MessageRoleAssistant, Content: "Go is great!"})

	store := memory.NewInMemoryStore()
	extractor := &staticExtractor{
		facts: []memory.Fact{{Content: "User likes Go"}},
	}

	ranger := &testSessionRanger{
		sessions: []struct{ id, agentID string }{
			{"sess1", "bot"},
		},
	}

	j := &MemoryExtractionJob{
		Logger:    slog.Default(),
		AgentID:   "bot",
		Sessions:  ranger,
		History:   history,
		Store:     store,
		Extractor: extractor,
	}

	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if extractor.calls != 1 {
		t.Errorf("extractor calls = %d, want 1", extractor.calls)
	}
	if store.Len() != 1 {
		t.Errorf("store len = %d, want 1", store.Len())
	}
}

func TestMemoryExtractionJob_Run_SkipsProcessed(t *testing.T) {
	t.Parallel()

	history := memory.NewInMemoryHistoryStore()
	_ = history.Append("sess1", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "Hello"})
	_ = history.Append("sess1", provider.LLMMessage{Role: provider.MessageRoleAssistant, Content: "Hi"})

	store := memory.NewInMemoryStore()
	extractor := &staticExtractor{
		facts: []memory.Fact{{Content: "greeting"}},
	}
	ranger := &testSessionRanger{
		sessions: []struct{ id, agentID string }{{"sess1", ""}},
	}

	j := &MemoryExtractionJob{
		Logger:    slog.Default(),
		Sessions:  ranger,
		History:   history,
		Store:     store,
		Extractor: extractor,
	}

	// First run: should extract.
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if extractor.calls != 1 {
		t.Fatalf("run 1: extractor calls = %d, want 1", extractor.calls)
	}

	// Second run without new messages: should skip.
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if extractor.calls != 1 {
		t.Errorf("run 2: extractor calls = %d, want 1 (no new messages)", extractor.calls)
	}

	// Append more messages, run again: should extract only new ones.
	_ = history.Append("sess1", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "What's Go?"})
	_ = history.Append("sess1", provider.LLMMessage{Role: provider.MessageRoleAssistant, Content: "A language"})

	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("run 3: %v", err)
	}
	if extractor.calls != 2 {
		t.Errorf("run 3: extractor calls = %d, want 2", extractor.calls)
	}
	if store.Len() != 2 {
		t.Errorf("store len = %d, want 2", store.Len())
	}
}

func TestMemoryExtractionJob_Run_FiltersByAgent(t *testing.T) {
	t.Parallel()

	history := memory.NewInMemoryHistoryStore()
	_ = history.Append("sess1", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "Hi"})
	_ = history.Append("sess1", provider.LLMMessage{Role: provider.MessageRoleAssistant, Content: "Hello"})
	_ = history.Append("sess2", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "Hey"})
	_ = history.Append("sess2", provider.LLMMessage{Role: provider.MessageRoleAssistant, Content: "Yo"})

	store := memory.NewInMemoryStore()
	extractor := &staticExtractor{
		facts: []memory.Fact{{Content: "fact"}},
	}
	ranger := &testSessionRanger{
		sessions: []struct{ id, agentID string }{
			{"sess1", "bot-a"},
			{"sess2", "bot-b"},
		},
	}

	j := &MemoryExtractionJob{
		Logger:    slog.Default(),
		AgentID:   "bot-a", // only process bot-a sessions
		Sessions:  ranger,
		History:   history,
		Store:     store,
		Extractor: extractor,
	}

	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only sess1 (bot-a) should be processed.
	if extractor.calls != 1 {
		t.Errorf("extractor calls = %d, want 1", extractor.calls)
	}
}

func TestMemoryExtractionJob_Run_CircuitBreaker(t *testing.T) {
	t.Parallel()

	history := memory.NewInMemoryHistoryStore()
	for i := range 5 {
		sid := fmt.Sprintf("sess%d", i)
		_ = history.Append(sid, provider.LLMMessage{Role: provider.MessageRoleUser, Content: "msg"})
		_ = history.Append(sid, provider.LLMMessage{Role: provider.MessageRoleAssistant, Content: "reply"})
	}

	store := memory.NewInMemoryStore()
	extractor := &staticExtractor{
		err: fmt.Errorf("LLM unavailable"),
	}
	sessions := make([]struct{ id, agentID string }, 5)
	for i := range 5 {
		sessions[i] = struct{ id, agentID string }{fmt.Sprintf("sess%d", i), ""}
	}
	ranger := &testSessionRanger{sessions: sessions}

	j := &MemoryExtractionJob{
		Logger:    slog.Default(),
		Sessions:  ranger,
		History:   history,
		Store:     store,
		Extractor: extractor,
	}

	// Should not return an error (circuit breaker stops iteration, doesn't fail the job).
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Circuit breaker triggers after 3 consecutive errors.
	if extractor.calls > 3 {
		t.Errorf("extractor calls = %d, want <= 3 (circuit breaker)", extractor.calls)
	}
}
