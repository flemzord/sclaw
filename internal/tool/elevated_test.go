package tool

import (
	"sync"
	"testing"
	"time"
)

type fakeTime struct {
	mu      sync.Mutex
	current time.Time
}

func newFakeTime() *fakeTime {
	return &fakeTime{current: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (f *fakeTime) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

func (f *fakeTime) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = f.current.Add(d)
}

func newTestElevated(ft *fakeTime) *ElevatedState {
	return &ElevatedState{now: ft.Now}
}

func TestElevatedState_InitiallyInactive(t *testing.T) {
	t.Parallel()

	e := NewElevatedState()
	if e.IsActive() {
		t.Error("new ElevatedState should be inactive")
	}
}

func TestElevatedState_ElevateAndExpire(t *testing.T) {
	t.Parallel()

	ft := newFakeTime()
	e := newTestElevated(ft)

	e.Elevate(5 * time.Minute)

	if !e.IsActive() {
		t.Error("should be active after Elevate")
	}

	ft.Advance(4 * time.Minute)
	if !e.IsActive() {
		t.Error("should still be active before expiry")
	}

	ft.Advance(2 * time.Minute)
	if e.IsActive() {
		t.Error("should be inactive after expiry")
	}
}

func TestElevatedState_Revoke(t *testing.T) {
	t.Parallel()

	ft := newFakeTime()
	e := newTestElevated(ft)

	e.Elevate(10 * time.Minute)
	if !e.IsActive() {
		t.Fatal("should be active")
	}

	e.Revoke()
	if e.IsActive() {
		t.Error("should be inactive after Revoke")
	}
}

func TestElevatedState_Apply_AskBecomesAllow(t *testing.T) {
	t.Parallel()

	ft := newFakeTime()
	e := newTestElevated(ft)
	e.Elevate(5 * time.Minute)

	got := e.Apply(ApprovalAsk)
	if got != ApprovalAllow {
		t.Errorf("elevated Apply(ask): got %q, want %q", got, ApprovalAllow)
	}
}

func TestElevatedState_Apply_DenyUnchanged(t *testing.T) {
	t.Parallel()

	ft := newFakeTime()
	e := newTestElevated(ft)
	e.Elevate(5 * time.Minute)

	got := e.Apply(ApprovalDeny)
	if got != ApprovalDeny {
		t.Errorf("elevated Apply(deny): got %q, want %q", got, ApprovalDeny)
	}
}

func TestElevatedState_Apply_AllowUnchanged(t *testing.T) {
	t.Parallel()

	ft := newFakeTime()
	e := newTestElevated(ft)
	e.Elevate(5 * time.Minute)

	got := e.Apply(ApprovalAllow)
	if got != ApprovalAllow {
		t.Errorf("elevated Apply(allow): got %q, want %q", got, ApprovalAllow)
	}
}

func TestElevatedState_Apply_InactiveNoChange(t *testing.T) {
	t.Parallel()

	e := NewElevatedState()

	got := e.Apply(ApprovalAsk)
	if got != ApprovalAsk {
		t.Errorf("inactive Apply(ask): got %q, want %q", got, ApprovalAsk)
	}
}

func TestElevatedState_ReElevate(t *testing.T) {
	t.Parallel()

	ft := newFakeTime()
	e := newTestElevated(ft)

	e.Elevate(2 * time.Minute)
	ft.Advance(1 * time.Minute)

	// Re-elevate with new duration.
	e.Elevate(5 * time.Minute)

	ft.Advance(3 * time.Minute)
	if !e.IsActive() {
		t.Error("should still be active after re-elevation")
	}

	ft.Advance(3 * time.Minute)
	if e.IsActive() {
		t.Error("should expire after re-elevated duration")
	}
}
