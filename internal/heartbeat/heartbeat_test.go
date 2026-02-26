package heartbeat

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// mockIterator implements SessionIterator for testing.
type mockIterator struct {
	sessions []mockSession
}

type mockSession struct {
	id         string
	lastActive time.Time
}

func (m *mockIterator) RangeActive(fn func(sessionID string, lastActive time.Time) bool) {
	for _, s := range m.sessions {
		if !fn(s.id, s.lastActive) {
			return
		}
	}
}

// mockPoker implements SessionPoker for testing.
type mockPoker struct {
	mu    sync.Mutex
	poked []string
	err   error
}

func (m *mockPoker) Poke(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.poked = append(m.poked, sessionID)
	return m.err
}

func (m *mockPoker) pokedSessions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	dst := make([]string, len(m.poked))
	copy(dst, m.poked)
	return dst
}

func TestParseQuietHours_Valid(t *testing.T) {
	t.Parallel()

	qh, err := ParseQuietHours("02:00-06:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if qh.Start != 2*time.Hour {
		t.Errorf("Start = %v, want %v", qh.Start, 2*time.Hour)
	}
	if qh.End != 6*time.Hour {
		t.Errorf("End = %v, want %v", qh.End, 6*time.Hour)
	}
}

func TestParseQuietHours_MidnightWrap(t *testing.T) {
	t.Parallel()

	qh, err := ParseQuietHours("23:00-07:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if qh.Start != 23*time.Hour {
		t.Errorf("Start = %v, want %v", qh.Start, 23*time.Hour)
	}
	if qh.End != 7*time.Hour {
		t.Errorf("End = %v, want %v", qh.End, 7*time.Hour)
	}
}

func TestParseQuietHours_Invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "missing dash", input: "0200 0600"},
		{name: "bad start format", input: "xx:00-06:00"},
		{name: "bad end format", input: "02:00-yy:00"},
		{name: "hour out of range", input: "25:00-06:00"},
		{name: "minute out of range", input: "02:60-06:00"},
		{name: "no colon in start", input: "0200-06:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseQuietHours(tt.input)
			if err == nil {
				t.Fatalf("expected error for input %q, got nil", tt.input)
			}
			if !errors.Is(err, ErrInvalidQuiet) {
				t.Errorf("expected ErrInvalidQuiet, got: %v", err)
			}
		})
	}
}

func TestQuietHours_IsQuiet_Normal(t *testing.T) {
	t.Parallel()

	qh := QuietHours{Start: 2 * time.Hour, End: 6 * time.Hour}

	// 03:00 should be quiet.
	quiet := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	if !qh.IsQuiet(quiet) {
		t.Error("03:00 should be quiet in 02:00-06:00")
	}

	// 08:00 should not be quiet.
	notQuiet := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	if qh.IsQuiet(notQuiet) {
		t.Error("08:00 should not be quiet in 02:00-06:00")
	}

	// 02:00 (boundary start) should be quiet.
	boundary := time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)
	if !qh.IsQuiet(boundary) {
		t.Error("02:00 should be quiet (inclusive start)")
	}

	// 06:00 (boundary end) should NOT be quiet.
	boundaryEnd := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)
	if qh.IsQuiet(boundaryEnd) {
		t.Error("06:00 should not be quiet (exclusive end)")
	}
}

func TestQuietHours_IsQuiet_MidnightWrap(t *testing.T) {
	t.Parallel()

	qh := QuietHours{Start: 23 * time.Hour, End: 7 * time.Hour}

	// 23:30 should be quiet.
	if !qh.IsQuiet(time.Date(2026, 1, 1, 23, 30, 0, 0, time.UTC)) {
		t.Error("23:30 should be quiet in 23:00-07:00")
	}

	// 01:00 should be quiet.
	if !qh.IsQuiet(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)) {
		t.Error("01:00 should be quiet in 23:00-07:00")
	}

	// 12:00 should not be quiet.
	if qh.IsQuiet(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) {
		t.Error("12:00 should not be quiet in 23:00-07:00")
	}
}

func TestHeartbeat_StartStop(t *testing.T) {
	t.Parallel()

	iter := &mockIterator{}
	poker := &mockPoker{}

	hb, err := New(Config{Interval: time.Hour}, iter, poker)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := hb.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := hb.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestHeartbeat_AlreadyStarted(t *testing.T) {
	t.Parallel()

	iter := &mockIterator{}
	poker := &mockPoker{}

	hb, err := New(Config{Interval: time.Hour}, iter, poker)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := hb.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = hb.Stop(ctx) })

	if err := hb.Start(ctx); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second Start = %v, want ErrAlreadyStarted", err)
	}
}

func TestHeartbeat_StopNotStarted(t *testing.T) {
	t.Parallel()

	iter := &mockIterator{}
	poker := &mockPoker{}

	hb, err := New(Config{Interval: time.Hour}, iter, poker)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := hb.Stop(context.Background()); !errors.Is(err, ErrNotStarted) {
		t.Errorf("Stop before Start = %v, want ErrNotStarted", err)
	}
}

func TestHeartbeat_Tick_PokesActiveSessions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	iter := &mockIterator{
		sessions: []mockSession{
			{id: "sess-1", lastActive: now.Add(-5 * time.Minute)},
			{id: "sess-2", lastActive: now.Add(-10 * time.Minute)},
		},
	}
	poker := &mockPoker{}

	hb, err := New(Config{
		Interval:   time.Hour,
		MaxIdleAge: 2 * time.Hour,
		Now:        func() time.Time { return now },
	}, iter, poker)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Directly call tick to test poke behavior without waiting for ticker.
	hb.tick(context.Background())

	poked := poker.pokedSessions()
	if len(poked) != 2 {
		t.Fatalf("poked %d sessions, want 2", len(poked))
	}
	if poked[0] != "sess-1" {
		t.Errorf("poked[0] = %q, want %q", poked[0], "sess-1")
	}
	if poked[1] != "sess-2" {
		t.Errorf("poked[1] = %q, want %q", poked[1], "sess-2")
	}
}

func TestHeartbeat_Tick_SkipsQuietHours(t *testing.T) {
	t.Parallel()

	// Set now to 03:00, quiet hours 02:00-06:00.
	now := time.Date(2026, 1, 15, 3, 0, 0, 0, time.UTC)
	qh := QuietHours{Start: 2 * time.Hour, End: 6 * time.Hour}

	iter := &mockIterator{
		sessions: []mockSession{
			{id: "sess-1", lastActive: now.Add(-1 * time.Minute)},
		},
	}
	poker := &mockPoker{}

	hb, err := New(Config{
		Interval:   time.Hour,
		QuietHours: &qh,
		Now:        func() time.Time { return now },
	}, iter, poker)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hb.tick(context.Background())

	poked := poker.pokedSessions()
	if len(poked) != 0 {
		t.Errorf("poked %d sessions during quiet hours, want 0", len(poked))
	}
}

func TestHeartbeat_Tick_SkipsIdleSessions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	iter := &mockIterator{
		sessions: []mockSession{
			{id: "active", lastActive: now.Add(-10 * time.Minute)},
			{id: "idle", lastActive: now.Add(-3 * time.Hour)},
		},
	}
	poker := &mockPoker{}

	hb, err := New(Config{
		Interval:   time.Hour,
		MaxIdleAge: 2 * time.Hour,
		Now:        func() time.Time { return now },
	}, iter, poker)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hb.tick(context.Background())

	poked := poker.pokedSessions()
	if len(poked) != 1 {
		t.Fatalf("poked %d sessions, want 1", len(poked))
	}
	if poked[0] != "active" {
		t.Errorf("poked[0] = %q, want %q", poked[0], "active")
	}
}

func TestNew_NilIterator(t *testing.T) {
	t.Parallel()

	_, err := New(Config{}, nil, &mockPoker{})
	if err == nil {
		t.Fatal("expected error for nil iterator")
	}
}

func TestNew_NilPoker(t *testing.T) {
	t.Parallel()

	_, err := New(Config{}, &mockIterator{}, nil)
	if err == nil {
		t.Fatal("expected error for nil poker")
	}
}
