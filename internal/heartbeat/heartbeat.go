// Package heartbeat provides a periodic poke mechanism for active sessions
// and a webhook handler for inbound message delivery.
package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Sentinel errors for heartbeat operations.
var (
	ErrAlreadyStarted = errors.New("heartbeat: already started")
	ErrNotStarted     = errors.New("heartbeat: not started")
	ErrInvalidQuiet   = errors.New("heartbeat: invalid quiet hours format")
)

// QuietHours defines a blackout window during which no heartbeats are sent.
// Format: "HH:MM-HH:MM" (24-hour). Supports midnight wrap (e.g., "23:00-07:00").
type QuietHours struct {
	Start time.Duration // offset from midnight
	End   time.Duration
}

// ParseQuietHours parses a "HH:MM-HH:MM" string into QuietHours.
func ParseQuietHours(s string) (QuietHours, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return QuietHours{}, fmt.Errorf("%w: expected HH:MM-HH:MM, got %q", ErrInvalidQuiet, s)
	}

	start, err := parseTimeOffset(strings.TrimSpace(parts[0]))
	if err != nil {
		return QuietHours{}, fmt.Errorf("%w: start: %w", ErrInvalidQuiet, err)
	}

	end, err := parseTimeOffset(strings.TrimSpace(parts[1]))
	if err != nil {
		return QuietHours{}, fmt.Errorf("%w: end: %w", ErrInvalidQuiet, err)
	}

	return QuietHours{Start: start, End: end}, nil
}

// parseTimeOffset parses "HH:MM" into a Duration from midnight.
func parseTimeOffset(s string) (time.Duration, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected HH:MM, got %q", s)
	}

	var h, m int
	if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
		return 0, fmt.Errorf("invalid hour: %q", parts[0])
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil {
		return 0, fmt.Errorf("invalid minute: %q", parts[1])
	}

	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("out of range: %02d:%02d", h, m)
	}

	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
}

// IsQuiet reports whether t falls within the quiet window.
// The caller is responsible for converting t to the desired timezone.
func (q QuietHours) IsQuiet(t time.Time) bool {
	offset := time.Duration(t.Hour())*time.Hour +
		time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second

	if q.Start <= q.End {
		// Normal range: e.g., 02:00-06:00
		return offset >= q.Start && offset < q.End
	}
	// Midnight wrap: e.g., 23:00-07:00
	return offset >= q.Start || offset < q.End
}

// SessionIterator enumerates active sessions (breaks router dependency).
type SessionIterator interface {
	RangeActive(fn func(sessionID string, lastActive time.Time) bool)
}

// SessionPoker delivers a heartbeat to a single session (breaks router dependency).
type SessionPoker interface {
	Poke(ctx context.Context, sessionID string) error
}

// Config holds heartbeat configuration.
type Config struct {
	Interval   time.Duration  // default 30m
	QuietHours *QuietHours    // nil = no quiet hours
	Timezone   *time.Location // nil = UTC
	MaxIdleAge time.Duration  // skip sessions idle longer than this
	Logger     *slog.Logger
	Now        func() time.Time // injectable for testing
}

func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = 30 * time.Minute
	}
	if c.Timezone == nil {
		c.Timezone = time.UTC
	}
	if c.MaxIdleAge <= 0 {
		c.MaxIdleAge = 2 * time.Hour
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.NewTextHandler(nil, nil))
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Heartbeat runs a dedicated goroutine that periodically pokes active sessions.
type Heartbeat struct {
	cfg      Config
	iterator SessionIterator
	poker    SessionPoker

	mu     sync.Mutex
	cancel context.CancelFunc
}

// New creates a Heartbeat with the given configuration.
func New(cfg Config, iterator SessionIterator, poker SessionPoker) (*Heartbeat, error) {
	if iterator == nil {
		return nil, errors.New("heartbeat: nil SessionIterator")
	}
	if poker == nil {
		return nil, errors.New("heartbeat: nil SessionPoker")
	}

	return &Heartbeat{
		cfg:      cfg.withDefaults(),
		iterator: iterator,
		poker:    poker,
	}, nil
}

// Start begins the heartbeat ticker loop. Returns ErrAlreadyStarted if called twice.
func (h *Heartbeat) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cancel != nil {
		return ErrAlreadyStarted
	}

	ctx, h.cancel = context.WithCancel(ctx)
	go h.run(ctx)
	return nil
}

// Stop gracefully stops the heartbeat loop. Returns ErrNotStarted if not running.
func (h *Heartbeat) Stop(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cancel == nil {
		return ErrNotStarted
	}

	h.cancel()
	h.cancel = nil
	return nil
}

// run is the main ticker loop.
func (h *Heartbeat) run(ctx context.Context) {
	ticker := time.NewTicker(h.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.tick(ctx)
		}
	}
}

// tick iterates sessions and pokes eligible ones.
func (h *Heartbeat) tick(ctx context.Context) {
	now := h.cfg.Now().In(h.cfg.Timezone)

	// Check quiet hours.
	if h.cfg.QuietHours != nil && h.cfg.QuietHours.IsQuiet(now) {
		h.cfg.Logger.Debug("heartbeat skipped: quiet hours")
		return
	}

	h.iterator.RangeActive(func(sessionID string, lastActive time.Time) bool {
		// Check context between iterations.
		if ctx.Err() != nil {
			return false
		}

		// Skip sessions idle longer than MaxIdleAge.
		if now.Sub(lastActive.In(h.cfg.Timezone)) > h.cfg.MaxIdleAge {
			return true
		}

		if err := h.poker.Poke(ctx, sessionID); err != nil {
			h.cfg.Logger.Warn("heartbeat poke failed",
				"session_id", sessionID,
				"error", err,
			)
		}

		return true
	})
}
