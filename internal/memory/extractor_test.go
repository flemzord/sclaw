package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
)

// Compile-time interface guards.
var (
	_ FactExtractor = (*LLMExtractor)(nil)
	_ FactExtractor = (*NopExtractor)(nil)
)

// mockProvider implements provider.Provider for testing.
type mockProvider struct {
	response provider.CompletionResponse
	err      error
}

func (m *mockProvider) Complete(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
	return m.response, m.err
}

func (m *mockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	return nil, nil
}

func (m *mockProvider) ContextWindowSize() int { return 128000 }
func (m *mockProvider) ModelName() string      { return "mock" }

func testExchange() Exchange {
	return Exchange{
		UserMessage:      provider.LLMMessage{Role: provider.MessageRoleUser, Content: "I prefer dark mode and use Go daily"},
		AssistantMessage: provider.LLMMessage{Role: provider.MessageRoleAssistant, Content: "Noted! Dark mode and Go are great choices."},
		Timestamp:        time.Now(),
	}
}

func TestLLMExtractor_Extract_ReturnsFacts(t *testing.T) {
	t.Parallel()

	mp := &mockProvider{
		response: provider.CompletionResponse{
			Content: "- User prefers dark mode\n- User uses Go daily",
		},
	}
	extractor := NewLLMExtractor(mp)

	facts, err := extractor.Extract(context.Background(), testExchange())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("got %d facts, want 2", len(facts))
	}
	if facts[0].Content != "User prefers dark mode" {
		t.Errorf("facts[0].Content = %q, want %q", facts[0].Content, "User prefers dark mode")
	}
	if facts[1].Content != "User uses Go daily" {
		t.Errorf("facts[1].Content = %q, want %q", facts[1].Content, "User uses Go daily")
	}
	// Verify IDs are non-empty.
	for i, f := range facts {
		if f.ID == "" {
			t.Errorf("facts[%d].ID is empty", i)
		}
		if f.CreatedAt.IsZero() {
			t.Errorf("facts[%d].CreatedAt is zero", i)
		}
	}
}

func TestLLMExtractor_Extract_NONE(t *testing.T) {
	t.Parallel()

	mp := &mockProvider{
		response: provider.CompletionResponse{Content: "NONE"},
	}
	extractor := NewLLMExtractor(mp)

	facts, err := extractor.Extract(context.Background(), testExchange())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if facts != nil {
		t.Fatalf("got %v, want nil", facts)
	}
}

func TestLLMExtractor_Extract_ProviderError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("provider failed")
	mp := &mockProvider{err: expectedErr}
	extractor := NewLLMExtractor(mp)

	facts, err := extractor.Extract(context.Background(), testExchange())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error wrapping %v, got %v", expectedErr, err)
	}
	if facts != nil {
		t.Fatalf("expected nil facts, got %v", facts)
	}
}

func TestNopExtractor_Extract(t *testing.T) {
	t.Parallel()

	extractor := NopExtractor{}

	facts, err := extractor.Extract(context.Background(), testExchange())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if facts != nil {
		t.Fatalf("got %v, want nil", facts)
	}
}

func TestParseExtractedFacts(t *testing.T) {
	t.Parallel()

	exchange := testExchange()

	tests := []struct {
		name      string
		response  string
		wantLen   int
		wantNil   bool
		wantFirst string
	}{
		{
			name:      "multiple bullet lines",
			response:  "- User prefers dark mode\n- User uses Go daily\n- User likes coffee",
			wantLen:   3,
			wantFirst: "User prefers dark mode",
		},
		{
			name:      "numbered list",
			response:  "1. User prefers dark mode\n2. User uses Go daily",
			wantLen:   2,
			wantFirst: "User prefers dark mode",
		},
		{
			name:      "asterisk bullets",
			response:  "* User prefers dark mode\n* User uses Go daily",
			wantLen:   2,
			wantFirst: "User prefers dark mode",
		},
		{
			name:     "empty response",
			response: "",
			wantNil:  true,
		},
		{
			name:     "NONE response",
			response: "NONE",
			wantNil:  true,
		},
		{
			name:      "mixed with empty lines",
			response:  "- fact one\n\n- fact two\n",
			wantLen:   2,
			wantFirst: "fact one",
		},
		{
			name:      "plain text single line",
			response:  "User likes Go",
			wantLen:   1,
			wantFirst: "User likes Go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			facts := parseExtractedFacts(tt.response, exchange)
			if tt.wantNil {
				if facts != nil {
					t.Fatalf("got %v, want nil", facts)
				}
				return
			}
			if len(facts) != tt.wantLen {
				t.Fatalf("got %d facts, want %d", len(facts), tt.wantLen)
			}
			if tt.wantFirst != "" && facts[0].Content != tt.wantFirst {
				t.Errorf("facts[0].Content = %q, want %q", facts[0].Content, tt.wantFirst)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantLen int
	}{
		{name: "empty", input: "", wantLen: 0},
		{name: "single line", input: "hello", wantLen: 1},
		{name: "two lines", input: "hello\nworld", wantLen: 2},
		{name: "trailing newline", input: "hello\n", wantLen: 1},
		{name: "blank lines filtered", input: "hello\n\nworld", wantLen: 2},
		{name: "whitespace only lines", input: "hello\n   \nworld", wantLen: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lines := splitLines(tt.input)
			if len(lines) != tt.wantLen {
				t.Fatalf("splitLines(%q): got %d lines, want %d: %v", tt.input, len(lines), tt.wantLen, lines)
			}
		})
	}
}

func TestTrimBullet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"- hello", "hello"},
		{"* hello", "hello"},
		{"1. hello", "hello"},
		{"12. hello", "hello"},
		{"hello", "hello"},
		{"", ""},
		{"-- double dash", "-- double dash"}, // not a valid bullet prefix
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			got := trimBullet(tt.input)
			if got != tt.want {
				t.Errorf("trimBullet(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
