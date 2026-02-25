package router

import (
	"sync"
	"time"
)

const defaultPruneInterval = 5 * time.Minute

// lazyPruner performs rate-limited session pruning.
// It runs at most once per interval to avoid excessive map iteration.
type lazyPruner struct {
	mu       sync.Mutex
	store    SessionStore
	laneLock *LaneLock
	maxIdle  time.Duration
	interval time.Duration
	lastRun  time.Time
	now      func() time.Time
}

type activeKeysProvider interface {
	ActiveKeys() map[SessionKey]struct{}
}

// newLazyPruner creates a pruner with the given configuration.
func newLazyPruner(store SessionStore, laneLock *LaneLock, maxIdle time.Duration) *lazyPruner {
	return &lazyPruner{
		store:    store,
		laneLock: laneLock,
		maxIdle:  maxIdle,
		interval: defaultPruneInterval,
		now:      time.Now,
	}
}

// TryPrune runs pruning if enough time has elapsed since the last run.
// Returns the number of sessions pruned, or 0 if rate-limited.
func (p *lazyPruner) TryPrune() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	if now.Sub(p.lastRun) < p.interval {
		return 0
	}
	p.lastRun = now

	pruned := p.store.Prune(p.maxIdle)

	// TOCTOU note: There is a deliberate gap between Prune (which removes
	// idle sessions from the store) and Cleanup (which garbage-collects
	// orphaned lane entries). This is safe because the lane's `refs` counter
	// ensures that any in-flight pipeline worker keeps its lane alive even
	// after the session is pruned from the store. Cleanup only marks
	// lanes as stale; actual deletion happens only when refs == 0.
	if p.laneLock != nil {
		if provider, ok := p.store.(activeKeysProvider); ok {
			p.laneLock.Cleanup(provider.ActiveKeys())
		}
	}

	return pruned
}
