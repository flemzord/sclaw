package tool

import (
	"sync"
	"time"
)

// ElevatedState tracks whether the session is in elevated mode.
// When active, "ask" policies are upgraded to "allow" (but "deny" is unchanged).
type ElevatedState struct {
	mu    sync.Mutex
	until time.Time
	now   func() time.Time // injectable for testing
}

// NewElevatedState creates a new ElevatedState with real time.
func NewElevatedState() *ElevatedState {
	return &ElevatedState{
		now: time.Now,
	}
}

// Elevate activates elevated mode for the given duration.
func (e *ElevatedState) Elevate(duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.until = e.now().Add(duration)
}

// Revoke immediately deactivates elevated mode.
func (e *ElevatedState) Revoke() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.until = time.Time{}
}

// IsActive reports whether elevated mode is currently active.
func (e *ElevatedState) IsActive() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.until.IsZero() && e.now().Before(e.until)
}

// Apply adjusts an approval level based on elevated state.
// If elevated is active, "ask" is upgraded to "allow".
// "deny" is never changed regardless of elevated state.
func (e *ElevatedState) Apply(level ApprovalLevel) ApprovalLevel {
	if level == ApprovalAsk && e.IsActive() {
		return ApprovalAllow
	}
	return level
}
