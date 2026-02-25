// Package reload provides configuration hot-reload via file polling and signal handling.
package reload

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const defaultPollInterval = 5 * time.Second

// WatcherConfig configures the file watcher.
type WatcherConfig struct {
	// ConfigPath is the path to the configuration file to watch.
	ConfigPath string

	// PollInterval is how often to check for file changes.
	// Defaults to 5 seconds if zero.
	PollInterval time.Duration
}

func (c WatcherConfig) pollIntervalOrDefault() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return defaultPollInterval
}

// EventType describes the type of file change event.
type EventType string

const (
	// EventModified indicates the config file was modified.
	EventModified EventType = "modified"
)

// Event represents a file change notification.
type Event struct {
	Type       EventType
	ConfigPath string
}

// Watcher polls a configuration file for modifications.
type Watcher struct {
	cfg     WatcherConfig
	events  chan Event
	stop    chan struct{}
	stopped chan struct{}

	started   atomic.Bool
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewWatcher creates a new file watcher.
func NewWatcher(cfg WatcherConfig) *Watcher {
	return &Watcher{
		cfg:     cfg,
		events:  make(chan Event, 1),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Start begins polling the config file for changes. Safe to call multiple
// times — only the first call starts the goroutine.
func (w *Watcher) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		w.started.Store(true)
		go w.poll(ctx)
	})
}

// Events returns the channel of file change events.
func (w *Watcher) Events() <-chan Event {
	return w.events
}

// Stop stops the watcher. Safe to call multiple times and before Start.
//
// Note: if Stop races with Start (called after startOnce.Do sets started=true
// but before the goroutine begins executing), Stop blocks on <-w.stopped until
// the goroutine starts, sees the closed stop channel, and exits. This is safe
// because the goroutine is guaranteed to be scheduled eventually.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stop)
	})
	if w.started.Load() {
		<-w.stopped
	}
}

func (w *Watcher) poll(ctx context.Context) {
	defer close(w.stopped)

	interval := w.cfg.pollIntervalOrDefault()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lastMod := w.statModTime()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			current := w.statModTime()
			if current.IsZero() {
				continue
			}
			if current.After(lastMod) {
				lastMod = current
				select {
				case w.events <- Event{
					Type:       EventModified,
					ConfigPath: w.cfg.ConfigPath,
				}:
				default:
					// Drop event if channel is full (debounce).
				}
			}
		}
	}
}

func (w *Watcher) statModTime() time.Time {
	info, err := os.Stat(w.cfg.ConfigPath)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
