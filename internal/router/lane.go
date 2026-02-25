package router

import "sync"

// LaneLock provides per-session serialization. It ensures that messages
// within the same session are processed one at a time (serial), while
// messages for different sessions can be processed concurrently (parallel).
//
// Design: a global mutex protects the lane map; each lane has its own
// mutex for intra-session serialization. The global mutex is held only
// briefly to look up or create the per-session mutex.
type LaneLock struct {
	mu    sync.Mutex
	lanes map[SessionKey]*lane
}

// lane stores per-session synchronization metadata.
// refs counts goroutines that acquired (or are waiting on) this lane.
// stale marks lanes eligible for cleanup once refs drops to zero.
type lane struct {
	mu    sync.Mutex
	refs  int
	stale bool
}

// NewLaneLock creates a ready-to-use LaneLock.
func NewLaneLock() *LaneLock {
	return &LaneLock{
		lanes: make(map[SessionKey]*lane),
	}
}

// Acquire gets or creates the per-session mutex and locks it.
// The caller must call Release with the same key when done.
func (l *LaneLock) Acquire(key SessionKey) {
	l.mu.Lock()
	ln, ok := l.lanes[key]
	if !ok {
		ln = &lane{}
		l.lanes[key] = ln
	}
	ln.refs++
	ln.stale = false
	l.mu.Unlock()

	// Lock outside the global mutex so other sessions are not blocked.
	ln.mu.Lock()
}

// Release unlocks the per-session mutex for the given key.
// The caller must have previously called Acquire with the same key.
func (l *LaneLock) Release(key SessionKey) {
	l.mu.Lock()
	ln, ok := l.lanes[key]
	if !ok {
		l.mu.Unlock()
		return
	}
	ln.refs--
	deleteNow := ln.refs == 0 && ln.stale
	if deleteNow {
		delete(l.lanes, key)
	}
	l.mu.Unlock()

	ln.mu.Unlock()
}

// Cleanup removes lane entries for sessions that are no longer active.
// activeKeys should contain only the keys of currently live sessions.
// This prevents unbounded growth of the lane map over time.
func (l *LaneLock) Cleanup(activeKeys map[SessionKey]struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for key, ln := range l.lanes {
		if _, active := activeKeys[key]; !active {
			ln.stale = true
			if ln.refs == 0 {
				delete(l.lanes, key)
			}
			continue
		}
		ln.stale = false
	}
}
