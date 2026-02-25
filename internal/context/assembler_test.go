package ctxengine_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	ctxengine "github.com/flemzord/sclaw/internal/context"
	"github.com/flemzord/sclaw/internal/provider"
)

// ---------------------------------------------------------------------------
// Basic assembly
// ---------------------------------------------------------------------------

func TestContextAssembler_Assemble_Basic(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens: 10000,
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"You are helpful"},
		Tools:       nil,
		History:     makeTestMessages(3),
		MemoryFacts: nil,
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.SystemPrompt != "You are helpful" {
		t.Errorf("SystemPrompt = %q, want %q", result.SystemPrompt, "You are helpful")
	}

	if len(result.Messages) != 3 {
		t.Errorf("len(Messages) = %d, want 3", len(result.Messages))
	}

	if result.Budget.WindowSize != 10000 {
		t.Errorf("Budget.WindowSize = %d, want 10000", result.Budget.WindowSize)
	}

	if result.Budget.System <= 0 {
		t.Errorf("Budget.System = %d, want > 0", result.Budget.System)
	}

	if result.Budget.Reserved != 1024 {
		t.Errorf("Budget.Reserved = %d, want 1024 (default)", result.Budget.Reserved)
	}

	if result.Compacted {
		t.Error("expected Compacted = false")
	}
}

func TestContextAssembler_Assemble_SystemPartsJoined(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{MaxContextTokens: 10000}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"Part A", "Part B", "Part C"},
		History:     makeTestMessages(1),
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Part A\n\nPart B\n\nPart C"
	if result.SystemPrompt != want {
		t.Errorf("SystemPrompt = %q, want %q", result.SystemPrompt, want)
	}
}

// ---------------------------------------------------------------------------
// Assembly with memory facts
// ---------------------------------------------------------------------------

func TestContextAssembler_Assemble_WithMemoryFacts(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens: 10000,
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"System prompt"},
		History:     makeTestMessages(1),
		MemoryFacts: []string{"User likes Go", "User prefers vim"},
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result.SystemPrompt, "System prompt") {
		t.Errorf("SystemPrompt should contain %q, got %q", "System prompt", result.SystemPrompt)
	}

	if !strings.Contains(result.SystemPrompt, "Relevant Memory") {
		t.Errorf("SystemPrompt should contain %q, got %q", "Relevant Memory", result.SystemPrompt)
	}

	if !strings.Contains(result.SystemPrompt, "User likes Go") {
		t.Errorf("SystemPrompt should contain %q, got %q", "User likes Go", result.SystemPrompt)
	}

	if !strings.Contains(result.SystemPrompt, "User prefers vim") {
		t.Errorf("SystemPrompt should contain %q, got %q", "User prefers vim", result.SystemPrompt)
	}
}

func TestContextAssembler_Assemble_MaxMemoryFacts(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens: 10000,
		MaxMemoryFacts:   2,
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"System"},
		History:     makeTestMessages(1),
		MemoryFacts: []string{"fact-1", "fact-2", "fact-3", "fact-4", "fact-5"},
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Count how many facts appear in the system prompt.
	count := 0
	for _, fact := range []string{"fact-1", "fact-2", "fact-3", "fact-4", "fact-5"} {
		if strings.Contains(result.SystemPrompt, fact) {
			count++
		}
	}

	if count != 2 {
		t.Errorf("expected 2 facts in SystemPrompt, found %d", count)
	}
}

func TestContextAssembler_Assemble_MaxMemoryTokens(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens: 10000,
		MaxMemoryTokens:  9,
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"System"},
		History:     makeTestMessages(1),
		MemoryFacts: []string{"fact-1", "fact-2"},
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result.SystemPrompt, "fact-1") {
		t.Fatalf("expected SystemPrompt to include %q, got %q", "fact-1", result.SystemPrompt)
	}
	if strings.Contains(result.SystemPrompt, "fact-2") {
		t.Fatalf("expected SystemPrompt to exclude %q due to MaxMemoryTokens, got %q", "fact-2", result.SystemPrompt)
	}
	if result.Budget.Memory <= 0 {
		t.Fatalf("Budget.Memory = %d, want > 0", result.Budget.Memory)
	}
	if result.Budget.Memory > 9 {
		t.Fatalf("Budget.Memory = %d, want <= 9", result.Budget.Memory)
	}
}

// ---------------------------------------------------------------------------
// Assembly with compaction (mock summarizer)
// ---------------------------------------------------------------------------

func TestContextAssembler_Assemble_TriggersCompactionWithSummarizer(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens:    100000,
		CompactionThreshold: 5,
		RetainRecent:        3,
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	summarizer := &mockSummarizer{result: "summary of old messages"}
	compactor := ctxengine.NewCompactor(summarizer, estimator, cfg)
	assembler.SetCompactor(compactor)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"System"},
		History:     makeTestMessages(10),
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Compacted {
		t.Error("expected Compacted = true")
	}

	// 1 summary + 3 retained = 4 messages
	if len(result.Messages) != 4 {
		t.Errorf("len(Messages) = %d, want 4", len(result.Messages))
	}

	if result.Messages[0].Role != provider.MessageRoleSystem {
		t.Errorf("first message role = %q, want %q", result.Messages[0].Role, provider.MessageRoleSystem)
	}

	if !strings.Contains(result.Messages[0].Content, "summary of old messages") {
		t.Errorf("first message should contain summary, got %q", result.Messages[0].Content)
	}

	if summarizer.called != 1 {
		t.Errorf("summarizer.called = %d, want 1", summarizer.called)
	}
}

func TestContextAssembler_Assemble_TriggersCompactionWithoutSummarizer(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens:    100000,
		CompactionThreshold: 5,
		RetainRecent:        3,
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	// nil summarizer → drop compaction
	compactor := ctxengine.NewCompactor(nil, estimator, cfg)
	assembler.SetCompactor(compactor)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"System"},
		History:     makeTestMessages(10),
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Compacted {
		t.Error("expected Compacted = true")
	}

	if len(result.Messages) != 3 {
		t.Errorf("len(Messages) = %d, want 3", len(result.Messages))
	}
}

func TestContextAssembler_Assemble_CompactionError(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens:    100000,
		CompactionThreshold: 5,
		RetainRecent:        3,
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	summarizer := &mockSummarizer{err: errors.New("summarizer broke")}
	compactor := ctxengine.NewCompactor(summarizer, estimator, cfg)
	assembler.SetCompactor(compactor)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"System"},
		History:     makeTestMessages(10),
	}

	_, err := assembler.Assemble(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from Assemble, got nil")
	}

	if !errors.Is(err, ctxengine.ErrCompactionFailed) {
		t.Errorf("error should wrap ErrCompactionFailed, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Assembly without compactor — large history gets trimmed to budget
// ---------------------------------------------------------------------------

func TestContextAssembler_Assemble_TruncatesHistory(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens: 200, // very tight budget
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"System"},
		History:     makeTestMessages(50),
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Messages) >= 50 {
		t.Errorf("expected history to be truncated, got len(Messages) = %d", len(result.Messages))
	}

	if len(result.Messages) < 1 {
		t.Error("expected at least 1 message after truncation")
	}

	if result.Compacted {
		t.Error("expected Compacted = false (no compactor set)")
	}
}

// ---------------------------------------------------------------------------
// trimHistory — with/without summary message at position 0
// ---------------------------------------------------------------------------

func TestContextAssembler_TrimHistory_PreservesSummary(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens: 150, // tight budget forces trimming
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	// Build history with a system summary at position 0.
	history := make([]provider.LLMMessage, 0, 20)
	history = append(history, provider.LLMMessage{
		Role:    provider.MessageRoleSystem,
		Content: "Previous conversation summary: user asked about Go",
	})
	history = append(history, makeTestMessages(19)...)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"S"},
		History:     history,
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The summary message should be preserved at position 0.
	if len(result.Messages) > 0 && result.Messages[0].Role == provider.MessageRoleSystem {
		if !strings.Contains(result.Messages[0].Content, "summary") {
			t.Errorf("expected summary message preserved, got %q", result.Messages[0].Content)
		}
	}

	// History should be trimmed.
	if len(result.Messages) >= 20 {
		t.Errorf("expected history to be trimmed, got %d messages", len(result.Messages))
	}
}

func TestContextAssembler_TrimHistory_NoSummary(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens: 150,
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"S"},
		History:     makeTestMessages(20),
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First message should be user (no system summary).
	if len(result.Messages) > 0 && result.Messages[0].Role == provider.MessageRoleSystem {
		t.Error("did not expect a system message at position 0 when no summary was present")
	}

	if len(result.Messages) >= 20 {
		t.Errorf("expected history to be trimmed, got %d messages", len(result.Messages))
	}
}

// ---------------------------------------------------------------------------
// Empty inputs
// ---------------------------------------------------------------------------

func TestContextAssembler_Assemble_EmptyRequest(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens: 1000,
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	req := ctxengine.AssemblyRequest{}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.SystemPrompt != "" {
		t.Errorf("SystemPrompt = %q, want empty string", result.SystemPrompt)
	}

	if len(result.Messages) != 0 {
		t.Errorf("len(Messages) = %d, want 0", len(result.Messages))
	}

	if len(result.Tools) != 0 {
		t.Errorf("len(Tools) = %d, want 0", len(result.Tools))
	}
}

func TestContextAssembler_Assemble_NoHistory(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{MaxContextTokens: 10000}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"You are helpful"},
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Messages) != 0 {
		t.Errorf("len(Messages) = %d, want 0", len(result.Messages))
	}

	if result.Budget.History != 0 {
		t.Errorf("Budget.History = %d, want 0", result.Budget.History)
	}
}

// ---------------------------------------------------------------------------
// Tools pass-through
// ---------------------------------------------------------------------------

func TestContextAssembler_Assemble_ToolsPassThrough(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{MaxContextTokens: 10000}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	tools := []provider.ToolDefinition{
		{
			Name:        "search",
			Description: "Search the web",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"System"},
		History:     makeTestMessages(1),
		Tools:       tools,
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(result.Tools))
	}

	if result.Tools[0].Name != "search" {
		t.Errorf("Tools[0].Name = %q, want %q", result.Tools[0].Name, "search")
	}

	if result.Budget.Tools <= 0 {
		t.Errorf("Budget.Tools = %d, want > 0", result.Budget.Tools)
	}
}

// ---------------------------------------------------------------------------
// Budget computation
// ---------------------------------------------------------------------------

func TestContextAssembler_Assemble_BudgetComputed(t *testing.T) {
	t.Parallel()

	estimator := ctxengine.NewCharEstimator(4.0)
	cfg := ctxengine.ContextConfig{
		MaxContextTokens: 10000,
		ReservedForReply: 512,
	}
	assembler := ctxengine.NewContextAssembler(estimator, cfg)

	req := ctxengine.AssemblyRequest{
		SystemParts: []string{"You are helpful"},
		History:     makeTestMessages(3),
	}

	result, err := assembler.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := result.Budget
	if b.WindowSize != 10000 {
		t.Errorf("Budget.WindowSize = %d, want 10000", b.WindowSize)
	}
	if b.Reserved != 512 {
		t.Errorf("Budget.Reserved = %d, want 512", b.Reserved)
	}
	if b.System <= 0 {
		t.Errorf("Budget.System = %d, want > 0", b.System)
	}
	if b.History <= 0 {
		t.Errorf("Budget.History = %d, want > 0", b.History)
	}
	if b.Used() <= 0 {
		t.Errorf("Budget.Used() = %d, want > 0", b.Used())
	}
	if b.Exceeded() {
		t.Error("Budget should not be exceeded with a 10000 token window")
	}
}
