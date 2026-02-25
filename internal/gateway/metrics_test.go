package gateway

import (
	"sync"
	"testing"
	"time"
)

func TestMetrics_RecordCompletion(t *testing.T) {
	t.Parallel()

	m := &Metrics{}
	m.RecordCompletion(100, 500*time.Millisecond)
	m.RecordCompletion(200, time.Second)

	snap := m.Snapshot()
	if snap.Completions != 2 {
		t.Errorf("Completions = %d, want 2", snap.Completions)
	}
	if snap.TotalTokens != 300 {
		t.Errorf("TotalTokens = %d, want 300", snap.TotalTokens)
	}
	if snap.AvgLatency != 750*time.Millisecond {
		t.Errorf("AvgLatency = %v, want 750ms", snap.AvgLatency)
	}
}

func TestMetrics_RecordMessage(t *testing.T) {
	t.Parallel()

	m := &Metrics{}
	m.RecordMessage()
	m.RecordMessage()
	m.RecordMessage()

	snap := m.Snapshot()
	if snap.Messages != 3 {
		t.Errorf("Messages = %d, want 3", snap.Messages)
	}
}

func TestMetrics_RecordError(t *testing.T) {
	t.Parallel()

	m := &Metrics{}
	m.RecordError()

	snap := m.Snapshot()
	if snap.Errors != 1 {
		t.Errorf("Errors = %d, want 1", snap.Errors)
	}
}

func TestMetrics_SnapshotEmpty(t *testing.T) {
	t.Parallel()

	m := &Metrics{}
	snap := m.Snapshot()

	if snap.Completions != 0 || snap.Messages != 0 || snap.Errors != 0 ||
		snap.TotalTokens != 0 || snap.AvgLatency != 0 {
		t.Errorf("empty snapshot should be all zeros: %+v", snap)
	}
}

func TestMetrics_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	m := &Metrics{}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			m.RecordCompletion(10, time.Millisecond)
		}()
		go func() {
			defer wg.Done()
			m.RecordMessage()
		}()
		go func() {
			defer wg.Done()
			m.RecordError()
		}()
	}
	wg.Wait()

	snap := m.Snapshot()
	if snap.Completions != 100 {
		t.Errorf("Completions = %d, want 100", snap.Completions)
	}
	if snap.Messages != 100 {
		t.Errorf("Messages = %d, want 100", snap.Messages)
	}
	if snap.Errors != 100 {
		t.Errorf("Errors = %d, want 100", snap.Errors)
	}
}
