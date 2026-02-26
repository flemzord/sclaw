package reload

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher_DetectsChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("initial"), 0o644); err != nil {
		t.Fatalf("writing initial file: %v", err)
	}

	w := NewWatcher(WatcherConfig{
		ConfigPath:   path,
		PollInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	// Wait for the watcher to read the initial modtime.
	time.Sleep(100 * time.Millisecond)

	// Modify the file.
	if err := os.WriteFile(path, []byte("modified"), 0o644); err != nil {
		t.Fatalf("writing modified file: %v", err)
	}

	select {
	case evt := <-w.Events():
		if evt.Type != EventModified {
			t.Errorf("got event type %q, want %q", evt.Type, EventModified)
		}
		if evt.ConfigPath != path {
			t.Errorf("got config path %q, want %q", evt.ConfigPath, path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for file change event")
	}
}

func TestWatcher_Stop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	w := NewWatcher(WatcherConfig{
		ConfigPath:   path,
		PollInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	// Stop should return promptly.
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return in time")
	}
}

func TestWatcher_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	w := NewWatcher(WatcherConfig{
		ConfigPath:   path,
		PollInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)

	// Cancel the context.
	cancel()

	// Stop should still work after context cancellation.
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after context cancel")
	}
}

func TestWatcher_StopBeforeStart(t *testing.T) {
	w := NewWatcher(WatcherConfig{
		ConfigPath:   "/any/path",
		PollInterval: 50 * time.Millisecond,
	})

	// Stop before Start should not deadlock.
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Stop before Start deadlocked")
	}
}

func TestWatcher_MissingFile(t *testing.T) {
	w := NewWatcher(WatcherConfig{
		ConfigPath:   "/nonexistent/file.yaml",
		PollInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	// Should not emit any events for a missing file.
	select {
	case evt := <-w.Events():
		t.Errorf("unexpected event: %+v", evt)
	case <-ctx.Done():
		// OK — no events.
	}
}
