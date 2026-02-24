package provider

import (
	"sync"
	"testing"
	"time"
)

func newTestTracker(cfg HealthConfig) (*healthTracker, *fakeTime) {
	h := newHealthTracker(cfg)
	ft := &fakeTime{current: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
	h.now = ft.Now
	return h, ft
}

type fakeTime struct {
	mu      sync.Mutex
	current time.Time
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

func TestHealthTracker_StartsHealthy(t *testing.T) {
	t.Parallel()
	h, _ := newTestTracker(HealthConfig{})
	if !h.IsAvailable() {
		t.Error("new tracker should be available")
	}
	if h.State() != stateHealthy {
		t.Error("new tracker should be healthy")
	}
}

func TestHealthTracker_AvailableAtExactExpiry(t *testing.T) {
	t.Parallel()
	h, ft := newTestTracker(HealthConfig{InitialBackoff: time.Second})

	h.RecordFailure()

	// Advance to the exact expiry moment.
	ft.Advance(time.Second)
	if !h.IsAvailable() {
		t.Error("should be available at exact expiry (>=, not >)")
	}
	if !h.ShouldHealthCheck() {
		t.Error("ShouldHealthCheck should be true at exact expiry")
	}
}

func TestHealthTracker_SingleFailureCooldown(t *testing.T) {
	t.Parallel()
	h, ft := newTestTracker(HealthConfig{InitialBackoff: time.Second})

	h.RecordFailure()

	if h.State() != stateCooldown {
		t.Fatalf("state = %d, want cooldown", h.State())
	}
	if h.IsAvailable() {
		t.Error("should not be available during cooldown")
	}

	ft.Advance(2 * time.Second)
	if !h.IsAvailable() {
		t.Error("should be available after cooldown expires")
	}
}

func TestHealthTracker_ExponentialBackoff(t *testing.T) {
	t.Parallel()
	h, ft := newTestTracker(HealthConfig{
		InitialBackoff: time.Second,
		MaxBackoff:     60 * time.Second,
		MaxFailures:    10,
	})

	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
	}

	for i, want := range expected {
		h.RecordFailure()
		if h.State() != stateCooldown {
			t.Fatalf("iteration %d: state = %d, want cooldown", i, h.State())
		}

		// Not available immediately
		if h.IsAvailable() {
			t.Errorf("iteration %d: should not be available before backoff", i)
		}

		// Available after backoff
		ft.Advance(want + time.Millisecond)
		if !h.IsAvailable() {
			t.Errorf("iteration %d: should be available after %v", i, want)
		}
	}
}

func TestHealthTracker_BackoffCap(t *testing.T) {
	t.Parallel()
	h, ft := newTestTracker(HealthConfig{
		InitialBackoff: 4 * time.Second,
		MaxBackoff:     10 * time.Second,
		MaxFailures:    10,
	})

	// First failure: 4s
	h.RecordFailure()
	ft.Advance(5 * time.Second)

	// Second failure: 8s
	h.RecordFailure()
	ft.Advance(9 * time.Second)

	// Third failure: would be 16s but capped at 10s
	h.RecordFailure()

	// Not available at 9s
	if h.IsAvailable() {
		t.Error("should not be available before capped backoff")
	}

	ft.Advance(11 * time.Second)
	if !h.IsAvailable() {
		t.Error("should be available after capped backoff")
	}
}

func TestHealthTracker_DeadAfterMaxFailures(t *testing.T) {
	t.Parallel()
	h, _ := newTestTracker(HealthConfig{MaxFailures: 3})

	for i := 0; i < 3; i++ {
		h.RecordFailure()
	}

	if h.State() != stateDead {
		t.Fatalf("state = %d, want dead", h.State())
	}
	if h.IsAvailable() {
		t.Error("dead provider should not be available")
	}
}

func TestHealthTracker_RecoveryFromDead(t *testing.T) {
	t.Parallel()
	h, _ := newTestTracker(HealthConfig{MaxFailures: 2})

	h.RecordFailure()
	h.RecordFailure()
	if h.State() != stateDead {
		t.Fatal("expected dead")
	}

	h.RecordSuccess()
	if h.State() != stateHealthy {
		t.Fatalf("state = %d, want healthy after recovery", h.State())
	}
	if !h.IsAvailable() {
		t.Error("should be available after recovery")
	}
	if h.Failures() != 0 {
		t.Errorf("failures = %d, want 0 after recovery", h.Failures())
	}
}

func TestHealthTracker_SuccessResetsBackoff(t *testing.T) {
	t.Parallel()
	h, ft := newTestTracker(HealthConfig{
		InitialBackoff: time.Second,
		MaxFailures:    10,
	})

	// Build up to 4s backoff
	h.RecordFailure()
	ft.Advance(2 * time.Second)
	h.RecordFailure()
	ft.Advance(3 * time.Second)

	// Reset
	h.RecordSuccess()

	// Next failure should start at 1s again
	h.RecordFailure()
	ft.Advance(500 * time.Millisecond)
	if h.IsAvailable() {
		t.Error("should not be available at 500ms (backoff should be 1s)")
	}
	ft.Advance(600 * time.Millisecond)
	if !h.IsAvailable() {
		t.Error("should be available at 1.1s")
	}
}

func TestHealthTracker_ShouldHealthCheck(t *testing.T) {
	t.Parallel()
	h, ft := newTestTracker(HealthConfig{MaxFailures: 3})

	if h.ShouldHealthCheck() {
		t.Error("healthy provider should not need health check")
	}

	// Cooldown — not expired
	h.RecordFailure()
	if h.ShouldHealthCheck() {
		t.Error("cooldown provider should not need check before expiry")
	}

	// Cooldown — expired
	ft.Advance(2 * time.Second)
	if !h.ShouldHealthCheck() {
		t.Error("expired cooldown should trigger health check")
	}

	// Dead
	h.RecordFailure()
	h.RecordFailure()
	if h.State() != stateDead {
		t.Fatal("expected dead")
	}
	if !h.ShouldHealthCheck() {
		t.Error("dead provider should need health check")
	}
}

func TestHealthTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	h, ft := newTestTracker(HealthConfig{MaxFailures: 100})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			h.RecordFailure()
		}()
		go func() {
			defer wg.Done()
			h.IsAvailable()
		}()
		go func() {
			defer wg.Done()
			ft.Advance(time.Millisecond)
			h.RecordSuccess()
		}()
	}
	wg.Wait()
}

func TestHealthConfig_Defaults(t *testing.T) {
	t.Parallel()

	cfg := HealthConfig{}
	cfg.defaults()

	if cfg.InitialBackoff != time.Second {
		t.Errorf("InitialBackoff = %v, want 1s", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 60*time.Second {
		t.Errorf("MaxBackoff = %v, want 60s", cfg.MaxBackoff)
	}
	if cfg.MaxFailures != 5 {
		t.Errorf("MaxFailures = %d, want 5", cfg.MaxFailures)
	}
	if cfg.CheckInterval != 10*time.Second {
		t.Errorf("CheckInterval = %v, want 10s", cfg.CheckInterval)
	}
}

func TestHealthConfig_CustomValues(t *testing.T) {
	t.Parallel()

	cfg := HealthConfig{
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     30 * time.Second,
		MaxFailures:    3,
		CheckInterval:  5 * time.Second,
	}
	cfg.defaults()

	if cfg.InitialBackoff != 500*time.Millisecond {
		t.Errorf("custom InitialBackoff overwritten: %v", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 30*time.Second {
		t.Errorf("custom MaxBackoff overwritten: %v", cfg.MaxBackoff)
	}
	if cfg.MaxFailures != 3 {
		t.Errorf("custom MaxFailures overwritten: %d", cfg.MaxFailures)
	}
}

func TestHealthConfig_InvalidValuesFallback(t *testing.T) {
	t.Parallel()

	cfg := HealthConfig{
		InitialBackoff: -1 * time.Second,
		MaxBackoff:     -2 * time.Second,
		MaxFailures:    -3,
		CheckInterval:  -4 * time.Second,
	}
	cfg.defaults()

	if cfg.InitialBackoff != time.Second {
		t.Errorf("InitialBackoff = %v, want 1s", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 60*time.Second {
		t.Errorf("MaxBackoff = %v, want 60s", cfg.MaxBackoff)
	}
	if cfg.MaxFailures != 5 {
		t.Errorf("MaxFailures = %d, want 5", cfg.MaxFailures)
	}
	if cfg.CheckInterval != 10*time.Second {
		t.Errorf("CheckInterval = %v, want 10s", cfg.CheckInterval)
	}
}

func TestHealthTracker_OnStateChange_Cooldown(t *testing.T) {
	t.Parallel()
	h, _ := newTestTracker(HealthConfig{MaxFailures: 3})

	var transitions []struct{ from, to healthState }
	h.onStateChange = func(from, to healthState) {
		transitions = append(transitions, struct{ from, to healthState }{from, to})
	}

	h.RecordFailure() // healthy → cooldown

	if len(transitions) != 1 {
		t.Fatalf("transitions = %d, want 1", len(transitions))
	}
	if transitions[0].from != stateHealthy || transitions[0].to != stateCooldown {
		t.Errorf("transition = %v→%v, want healthy→cooldown", transitions[0].from, transitions[0].to)
	}
}

func TestHealthTracker_OnStateChange_Dead(t *testing.T) {
	t.Parallel()
	h, _ := newTestTracker(HealthConfig{MaxFailures: 2})

	var transitions []struct{ from, to healthState }
	h.onStateChange = func(from, to healthState) {
		transitions = append(transitions, struct{ from, to healthState }{from, to})
	}

	h.RecordFailure() // healthy → cooldown
	h.RecordFailure() // cooldown → dead

	if len(transitions) != 2 {
		t.Fatalf("transitions = %d, want 2", len(transitions))
	}
	if transitions[1].from != stateCooldown || transitions[1].to != stateDead {
		t.Errorf("transition = %v→%v, want cooldown→dead", transitions[1].from, transitions[1].to)
	}
}

func TestHealthTracker_OnStateChange_Revival(t *testing.T) {
	t.Parallel()
	h, _ := newTestTracker(HealthConfig{MaxFailures: 2})

	var transitions []struct{ from, to healthState }
	h.onStateChange = func(from, to healthState) {
		transitions = append(transitions, struct{ from, to healthState }{from, to})
	}

	h.RecordFailure() // healthy → cooldown
	h.RecordFailure() // cooldown → dead
	h.RecordSuccess() // dead → healthy

	if len(transitions) != 3 {
		t.Fatalf("transitions = %d, want 3", len(transitions))
	}
	if transitions[2].from != stateDead || transitions[2].to != stateHealthy {
		t.Errorf("transition = %v→%v, want dead→healthy", transitions[2].from, transitions[2].to)
	}
}

func TestHealthTracker_OnStateChange_NotCalledWhenNoChange(t *testing.T) {
	t.Parallel()
	h, _ := newTestTracker(HealthConfig{})

	called := false
	h.onStateChange = func(_, _ healthState) {
		called = true
	}

	// Already healthy, RecordSuccess should not trigger callback.
	h.RecordSuccess()

	if called {
		t.Error("onStateChange should not be called when state does not change")
	}
}

func TestHealthTracker_CurrentBackoff(t *testing.T) {
	t.Parallel()
	h, ft := newTestTracker(HealthConfig{InitialBackoff: time.Second, MaxFailures: 10})

	if h.CurrentBackoff() != 0 {
		t.Errorf("initial backoff = %v, want 0", h.CurrentBackoff())
	}

	h.RecordFailure()
	if h.CurrentBackoff() != time.Second {
		t.Errorf("backoff after 1st failure = %v, want 1s", h.CurrentBackoff())
	}

	ft.Advance(2 * time.Second)
	h.RecordFailure()
	if h.CurrentBackoff() != 2*time.Second {
		t.Errorf("backoff after 2nd failure = %v, want 2s", h.CurrentBackoff())
	}
}

func TestHealthState_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state healthState
		want  string
	}{
		{stateHealthy, "healthy"},
		{stateCooldown, "cooldown"},
		{stateDead, "dead"},
		{healthState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("healthState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestHealthConfig_CheckIntervalOrDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  HealthConfig
		want time.Duration
	}{
		{"zero", HealthConfig{}, 10 * time.Second},
		{"negative", HealthConfig{CheckInterval: -1}, 10 * time.Second},
		{"custom", HealthConfig{CheckInterval: 5 * time.Second}, 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.cfg.checkIntervalOrDefault(); got != tt.want {
				t.Errorf("checkIntervalOrDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}
