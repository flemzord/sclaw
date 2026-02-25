package agent

import (
	"encoding/json"
	"testing"

	"github.com/flemzord/sclaw/internal/provider"
)

func TestLoopDetector_BelowThreshold(t *testing.T) {
	t.Parallel()
	d := newLoopDetector(3)

	if d.record("read", json.RawMessage(`{"file":"a.txt"}`)) {
		t.Error("expected no loop on first call")
	}
	if d.record("read", json.RawMessage(`{"file":"a.txt"}`)) {
		t.Error("expected no loop on second call")
	}
}

func TestLoopDetector_AtThreshold(t *testing.T) {
	t.Parallel()
	d := newLoopDetector(3)

	d.record("read", json.RawMessage(`{"file":"a.txt"}`))
	d.record("read", json.RawMessage(`{"file":"a.txt"}`))

	if !d.record("read", json.RawMessage(`{"file":"a.txt"}`)) {
		t.Error("expected loop detected at threshold")
	}
}

func TestLoopDetector_DifferentArgs(t *testing.T) {
	t.Parallel()
	d := newLoopDetector(2)

	d.record("read", json.RawMessage(`{"file":"a.txt"}`))
	if d.record("read", json.RawMessage(`{"file":"b.txt"}`)) {
		t.Error("different args should not trigger loop")
	}
}

func TestLoopDetector_Reset(t *testing.T) {
	t.Parallel()
	d := newLoopDetector(2)

	d.record("read", json.RawMessage(`{"file":"a.txt"}`))
	d.reset()

	if d.record("read", json.RawMessage(`{"file":"a.txt"}`)) {
		t.Error("expected no loop after reset")
	}
}

func TestTokenTracker_Add(t *testing.T) {
	t.Parallel()
	tr := newTokenTracker(1000)

	tr.add(provider.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	tr.add(provider.TokenUsage{PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300})

	got := tr.total()
	if got.PromptTokens != 300 {
		t.Errorf("PromptTokens = %d, want 300", got.PromptTokens)
	}
	if got.CompletionTokens != 150 {
		t.Errorf("CompletionTokens = %d, want 150", got.CompletionTokens)
	}
	if got.TotalTokens != 450 {
		t.Errorf("TotalTokens = %d, want 450", got.TotalTokens)
	}
}

func TestTokenTracker_Exceeded(t *testing.T) {
	t.Parallel()
	tr := newTokenTracker(500)

	tr.add(provider.TokenUsage{TotalTokens: 500})
	if !tr.exceeded() {
		t.Error("expected exceeded at budget")
	}
}

func TestTokenTracker_UnlimitedBudget(t *testing.T) {
	t.Parallel()
	tr := newTokenTracker(0)

	tr.add(provider.TokenUsage{TotalTokens: 999999})
	if tr.exceeded() {
		t.Error("zero budget should never exceed")
	}
}
