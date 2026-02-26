package cron

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

// testSessionStore implements SessionStore for job tests.
type testSessionStore struct {
	pruneCalls atomic.Int32
	pruneFunc  func(maxIdle time.Duration) int
}

func (s *testSessionStore) Prune(maxIdle time.Duration) int {
	s.pruneCalls.Add(1)
	if s.pruneFunc != nil {
		return s.pruneFunc(maxIdle)
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

func TestSessionCleanupJob_Schedule(t *testing.T) {
	t.Parallel()
	j := &SessionCleanupJob{Logger: slog.Default()}
	if j.Schedule() != "*/5 * * * *" {
		t.Errorf("schedule = %q, want %q", j.Schedule(), "*/5 * * * *")
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

func TestMemoryExtractionJob_Name(t *testing.T) {
	t.Parallel()
	j := &MemoryExtractionJob{Logger: slog.Default()}
	if j.Name() != "memory_extraction" {
		t.Errorf("name = %q, want %q", j.Name(), "memory_extraction")
	}
}

func TestMemoryExtractionJob_Schedule(t *testing.T) {
	t.Parallel()
	j := &MemoryExtractionJob{Logger: slog.Default()}
	if j.Schedule() != "*/10 * * * *" {
		t.Errorf("schedule = %q, want %q", j.Schedule(), "*/10 * * * *")
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

func TestMemoryCompactionJob_Schedule(t *testing.T) {
	t.Parallel()
	j := &MemoryCompactionJob{Logger: slog.Default()}
	if j.Schedule() != "0 * * * *" {
		t.Errorf("schedule = %q, want %q", j.Schedule(), "0 * * * *")
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
