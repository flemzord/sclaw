// Package crontest provides test doubles for the cron package.
package crontest

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flemzord/sclaw/internal/cron"
)

// MockJob is a configurable test double for cron.Job.
type MockJob struct {
	NameVal     string
	ScheduleVal string
	RunFunc     func(ctx context.Context) error

	mu       sync.Mutex
	calls    int
	lastCall time.Time
}

// Compile-time interface check.
var _ cron.Job = (*MockJob)(nil)

// Name implements cron.Job.
func (m *MockJob) Name() string { return m.NameVal }

// Schedule implements cron.Job.
func (m *MockJob) Schedule() string { return m.ScheduleVal }

// Run implements cron.Job and increments the call counter.
func (m *MockJob) Run(ctx context.Context) error {
	m.mu.Lock()
	m.calls++
	m.lastCall = time.Now()
	m.mu.Unlock()

	if m.RunFunc != nil {
		return m.RunFunc(ctx)
	}
	return nil
}

// CallCount returns the number of times Run was called.
func (m *MockJob) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// LastCall returns the time of the last Run call.
func (m *MockJob) LastCall() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastCall
}

// MockSessionStore is a test double for cron.SessionStore.
type MockSessionStore struct {
	PruneFunc  func(maxIdle time.Duration) int
	PruneCalls atomic.Int32
}

// Prune implements cron.SessionStore.
func (m *MockSessionStore) Prune(maxIdle time.Duration) int {
	m.PruneCalls.Add(1)
	if m.PruneFunc != nil {
		return m.PruneFunc(maxIdle)
	}
	return 0
}
