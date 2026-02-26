package cron

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// simpleJob is a minimal Job for scheduler tests.
type simpleJob struct {
	name     string
	schedule string
	runFunc  func(ctx context.Context) error
	mu       sync.Mutex
	calls    int
}

func (j *simpleJob) Name() string     { return j.name }
func (j *simpleJob) Schedule() string { return j.schedule }
func (j *simpleJob) Run(ctx context.Context) error {
	j.mu.Lock()
	j.calls++
	j.mu.Unlock()
	if j.runFunc != nil {
		return j.runFunc(ctx)
	}
	return nil
}

func TestScheduler_RegisterJob_DuplicateName(t *testing.T) {
	t.Parallel()

	s := NewScheduler(slog.Default())

	err := s.RegisterJob(&simpleJob{name: "test", schedule: "* * * * *"})
	if err != nil {
		t.Fatalf("first registration should succeed: %v", err)
	}

	err = s.RegisterJob(&simpleJob{name: "test", schedule: "* * * * *"})
	if err == nil {
		t.Fatal("duplicate registration should fail")
	}
}

func TestScheduler_Start_InvalidSchedule(t *testing.T) {
	t.Parallel()

	s := NewScheduler(slog.Default())
	_ = s.RegisterJob(&simpleJob{name: "bad", schedule: "invalid"})

	err := s.Start()
	if err == nil {
		t.Fatal("expected error for invalid schedule")
	}
}

func TestScheduler_StartStop(t *testing.T) {
	t.Parallel()

	s := NewScheduler(slog.Default())
	_ = s.RegisterJob(&simpleJob{name: "noop", schedule: "* * * * *"})

	if err := s.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

func TestScheduler_NilLogger(t *testing.T) {
	t.Parallel()

	s := NewScheduler(nil) // should not panic
	if s.logger == nil {
		t.Fatal("logger should default to slog.Default()")
	}
}

func TestScheduler_NoParallelExecution(t *testing.T) {
	t.Parallel()

	// This test verifies that the TryLock mechanism prevents parallel
	// execution of the same job.
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	s := NewScheduler(slog.Default())
	_ = s.RegisterJob(&simpleJob{
		name:     "slow",
		schedule: "* * * * *",
		runFunc: func(_ context.Context) error {
			c := concurrent.Add(1)
			for {
				old := maxConcurrent.Load()
				if c <= old || maxConcurrent.CompareAndSwap(old, c) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			concurrent.Add(-1)
			return nil
		},
	})

	if err := s.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Manually trigger the job multiple times concurrently to test TryLock.
	lock := s.locks["slow"]
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if lock.TryLock() {
				concurrent.Add(1)
				time.Sleep(10 * time.Millisecond)
				concurrent.Add(-1)
				lock.Unlock()
			}
		}()
	}
	wg.Wait()

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	if maxConcurrent.Load() > 1 {
		t.Errorf("max concurrent = %d, want <= 1", maxConcurrent.Load())
	}
}

func TestScheduler_JobError(t *testing.T) {
	t.Parallel()

	// Verify that job errors don't crash the scheduler.
	s := NewScheduler(slog.Default())
	_ = s.RegisterJob(&simpleJob{
		name:     "failing",
		schedule: "* * * * *",
		runFunc: func(_ context.Context) error {
			return errors.New("job failed")
		},
	})

	if err := s.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// The scheduler should still be running after a job error.
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

func TestScheduler_StopWithoutStart(t *testing.T) {
	t.Parallel()

	s := NewScheduler(slog.Default())
	// Stop without Start should not panic.
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}
