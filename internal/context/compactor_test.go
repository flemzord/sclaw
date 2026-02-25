package ctxengine_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ctxengine "github.com/flemzord/sclaw/internal/context"
	"github.com/flemzord/sclaw/internal/provider"
)

func TestCompactor_ShouldCompact(t *testing.T) {
	t.Parallel()

	cfg := ctxengine.ContextConfig{CompactionThreshold: 5}
	c := ctxengine.NewCompactor(nil, &mockEstimator{}, cfg)

	tests := []struct {
		name     string
		msgCount int
		want     bool
	}{
		{"above threshold", 6, true},
		{"at threshold", 5, false},
		{"below threshold", 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			history := makeTestMessages(tt.msgCount)
			if got := c.ShouldCompact(history); got != tt.want {
				t.Errorf("ShouldCompact(%d messages) = %v, want %v", tt.msgCount, got, tt.want)
			}
		})
	}
}

func TestCompactor_Compact_WithSummarizer(t *testing.T) {
	t.Parallel()

	cfg := ctxengine.ContextConfig{
		RetainRecent:        3,
		CompactionThreshold: 5,
	}
	summarizer := &mockSummarizer{result: "summary of old messages"}
	c := ctxengine.NewCompactor(summarizer, &mockEstimator{}, cfg)

	history := makeTestMessages(8)
	result, err := c.Compact(context.Background(), history)
	if err != nil {
		t.Fatalf("Compact returned unexpected error: %v", err)
	}

	// Expect 1 summary message + 3 retained = 4 messages.
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	// First message should be the system-role summary.
	if result[0].Role != provider.MessageRoleSystem {
		t.Errorf("first message role = %q, want %q", result[0].Role, provider.MessageRoleSystem)
	}
	wantContent := "[Conversation Summary]\nsummary of old messages"
	if result[0].Content != wantContent {
		t.Errorf("first message content = %q, want %q", result[0].Content, wantContent)
	}

	// Remaining messages should be the last 3 from history.
	for i := 1; i < 4; i++ {
		expected := history[len(history)-3+(i-1)]
		if !reflect.DeepEqual(result[i], expected) {
			t.Errorf("result[%d] = %+v, want %+v", i, result[i], expected)
		}
	}
}

func TestCompactor_Compact_WithoutSummarizer(t *testing.T) {
	t.Parallel()

	cfg := ctxengine.ContextConfig{RetainRecent: 3}
	c := ctxengine.NewCompactor(nil, &mockEstimator{}, cfg)

	history := makeTestMessages(8)
	result, err := c.Compact(context.Background(), history)
	if err != nil {
		t.Fatalf("Compact returned unexpected error: %v", err)
	}

	// Should return only the last 3 messages, no summary.
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	for i, msg := range result {
		expected := history[len(history)-3+i]
		if !reflect.DeepEqual(msg, expected) {
			t.Errorf("result[%d] = %+v, want %+v", i, msg, expected)
		}
	}
}

func TestCompactor_Compact_HistoryUnderRetain(t *testing.T) {
	t.Parallel()

	cfg := ctxengine.ContextConfig{RetainRecent: 5}
	c := ctxengine.NewCompactor(nil, &mockEstimator{}, cfg)

	history := makeTestMessages(2)
	result, err := c.Compact(context.Background(), history)
	if err != nil {
		t.Fatalf("Compact returned unexpected error: %v", err)
	}

	if len(result) != len(history) {
		t.Fatalf("expected %d messages, got %d", len(history), len(result))
	}

	for i, msg := range result {
		if !reflect.DeepEqual(msg, history[i]) {
			t.Errorf("result[%d] = %+v, want %+v", i, msg, history[i])
		}
	}
}

func TestCompactor_Compact_SummarizerError(t *testing.T) {
	t.Parallel()

	cfg := ctxengine.ContextConfig{RetainRecent: 3}
	summarizeErr := errors.New("summarizer failed")
	summarizer := &mockSummarizer{err: summarizeErr}
	c := ctxengine.NewCompactor(summarizer, &mockEstimator{}, cfg)

	history := makeTestMessages(8)
	_, err := c.Compact(context.Background(), history)
	if err == nil {
		t.Fatal("expected error from Compact, got nil")
	}

	if !errors.Is(err, summarizeErr) {
		t.Errorf("error chain does not contain summarizer error: %v", err)
	}
}

func TestCompactor_EmergencyCompact(t *testing.T) {
	t.Parallel()

	cfg := ctxengine.ContextConfig{EmergencyRetain: 2}
	c := ctxengine.NewCompactor(nil, &mockEstimator{}, cfg)

	history := makeTestMessages(10)
	result, err := c.EmergencyCompact(context.Background(), history)
	if err != nil {
		t.Fatalf("EmergencyCompact returned unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}

	for i, msg := range result {
		expected := history[len(history)-2+i]
		if !reflect.DeepEqual(msg, expected) {
			t.Errorf("result[%d] = %+v, want %+v", i, msg, expected)
		}
	}

	// Verify it's a copy: modifying result should not affect original.
	originalContent := history[len(history)-2].Content
	result[0].Content = "modified"
	if history[len(history)-2].Content != originalContent {
		t.Error("modifying result affected original history; expected a copy")
	}
}

func TestCompactor_EmergencyCompact_ShortHistory(t *testing.T) {
	t.Parallel()

	cfg := ctxengine.ContextConfig{EmergencyRetain: 5}
	c := ctxengine.NewCompactor(nil, &mockEstimator{}, cfg)

	history := makeTestMessages(1)
	result, err := c.EmergencyCompact(context.Background(), history)
	if err != nil {
		t.Fatalf("EmergencyCompact returned unexpected error: %v", err)
	}

	if len(result) != len(history) {
		t.Fatalf("expected %d messages, got %d", len(history), len(result))
	}

	if !reflect.DeepEqual(result[0], history[0]) {
		t.Errorf("result[0] = %+v, want %+v", result[0], history[0])
	}
}
