package memory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	ctxengine "github.com/flemzord/sclaw/internal/context"
	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/provider"
)

// integrationProvider is a mock provider for integration tests.
type integrationProvider struct {
	extractResponse string
	summarizeFunc   func(messages []provider.LLMMessage) string
}

func (p *integrationProvider) Complete(_ context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	// Determine if this is an extraction or summarization request.
	if len(req.Messages) > 0 {
		content := req.Messages[0].Content
		// If the prompt contains "Analyze the following exchange", it's extraction.
		if strings.Contains(content, "extract") || strings.Contains(content, "Facts:") {
			return provider.CompletionResponse{Content: p.extractResponse}, nil
		}
	}
	// Otherwise treat as summarization.
	if p.summarizeFunc != nil {
		return provider.CompletionResponse{Content: p.summarizeFunc(req.Messages)}, nil
	}
	return provider.CompletionResponse{Content: "Summary of conversation."}, nil
}

func (p *integrationProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	return nil, nil
}

func (p *integrationProvider) ContextWindowSize() int { return 128000 }
func (p *integrationProvider) ModelName() string      { return "integration-mock" }

// TestIntegration_ExchangeToExtractionToInjection tests the full flow:
// exchange → extraction → storage → injection.
func TestIntegration_ExchangeToExtractionToInjection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	estimator := ctxengine.NewCharEstimator(4.0)

	// Set up stores.
	historyStore := memory.NewInMemoryHistoryStore()
	memoryStore := memory.NewInMemoryStore()

	// Set up extractor with a mock provider that returns facts.
	mockProvider := &integrationProvider{
		extractResponse: "- User prefers dark mode\n- User's name is Alice\n- User works at Acme Corp",
	}
	extractor := memory.NewLLMExtractor(mockProvider)

	// Step 1: Simulate an exchange.
	sessionID := "session-integration-1"
	userMsg := provider.LLMMessage{
		Role:    provider.MessageRoleUser,
		Content: "Hi, I'm Alice. I work at Acme Corp and I prefer dark mode.",
	}
	assistantMsg := provider.LLMMessage{
		Role:    provider.MessageRoleAssistant,
		Content: "Nice to meet you, Alice! I'll remember your preferences.",
	}

	// Step 2: Store in history.
	if err := historyStore.Append(sessionID, userMsg); err != nil {
		t.Fatalf("append user msg: %v", err)
	}
	if err := historyStore.Append(sessionID, assistantMsg); err != nil {
		t.Fatalf("append assistant msg: %v", err)
	}

	// Verify history.
	n, err := historyStore.Len(sessionID)
	if err != nil {
		t.Fatalf("history len: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 messages, got %d", n)
	}

	// Step 3: Extract facts from the exchange.
	exchange := memory.Exchange{
		SessionID:        sessionID,
		UserMessage:      userMsg,
		AssistantMessage: assistantMsg,
		Timestamp:        time.Now(),
	}

	facts, err := extractor.Extract(ctx, exchange)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(facts) != 3 {
		t.Fatalf("expected 3 facts, got %d", len(facts))
	}

	// Step 4: Store facts in memory (Source is set by the extractor via Exchange.SessionID).
	for i := range facts {
		if err := memoryStore.Index(ctx, facts[i]); err != nil {
			t.Fatalf("index fact %d: %v", i, err)
		}
	}

	if memoryStore.Len() != 3 {
		t.Fatalf("expected 3 facts in store, got %d", memoryStore.Len())
	}

	// Step 5: Inject memory for a new query.
	injected, err := memory.InjectMemory(ctx, memory.InjectionRequest{
		Store: memoryStore, Query: "dark mode", MaxFacts: 10, MaxTokens: 2000, Estimator: estimator,
	})
	if err != nil {
		t.Fatalf("inject: %v", err)
	}

	if len(injected) == 0 {
		t.Fatal("expected at least one injected fact")
	}

	// Verify the injected fact mentions "dark mode".
	found := false
	for _, fact := range injected {
		if strings.Contains(fact, "dark mode") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected injected facts to contain 'dark mode', got %v", injected)
	}
}

// TestIntegration_CompactionAndAssembly tests the full context engine flow:
// history accumulation → compaction → assembly.
func TestIntegration_CompactionAndAssembly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	estimator := ctxengine.NewCharEstimator(4.0)

	// Use a low compaction threshold for testing.
	cfg := ctxengine.ContextConfig{
		CompactionThreshold: 5,
		RetainRecent:        3,
		EmergencyRetain:     2,
		ReservedForReply:    100,
	}

	// Mock summarizer via the integrationProvider.
	mockProv := &integrationProvider{
		summarizeFunc: func(_ []provider.LLMMessage) string {
			return "User discussed weather and Go programming."
		},
	}
	summarizer := &providerSummarizer{provider: mockProv}

	compactor := ctxengine.NewCompactor(summarizer, estimator, cfg)
	assembler := ctxengine.NewContextAssembler(estimator, cfg)
	assembler.SetCompactor(compactor)

	// Build a history with 8 messages (exceeds threshold of 5).
	var history []provider.LLMMessage
	for i := 0; i < 4; i++ {
		history = append(history,
			provider.LLMMessage{
				Role:    provider.MessageRoleUser,
				Content: "User message about topic " + string(rune('A'+i)),
			},
			provider.LLMMessage{
				Role:    provider.MessageRoleAssistant,
				Content: "Assistant response about topic " + string(rune('A'+i)),
			},
		)
	}

	if len(history) != 8 {
		t.Fatalf("expected 8 messages, got %d", len(history))
	}

	// Assemble context — should trigger compaction.
	result, err := assembler.Assemble(ctx, ctxengine.AssemblyRequest{
		WindowSize:  8000,
		SystemParts: []string{"You are a helpful assistant."},
		History:     history,
	})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	if !result.Compacted {
		t.Error("expected compaction to be triggered")
	}

	// After compaction: 1 summary + 3 retained = 4 messages.
	if len(result.Messages) != 4 {
		t.Errorf("expected 4 messages after compaction, got %d", len(result.Messages))
	}

	// First message should be the summary.
	if result.Messages[0].Role != provider.MessageRoleSystem {
		t.Errorf("expected first message to be system (summary), got %s", result.Messages[0].Role)
	}
	if !strings.Contains(result.Messages[0].Content, "User discussed weather") {
		t.Errorf("expected summary content, got %q", result.Messages[0].Content)
	}

	// Budget should be computed.
	if result.Budget.WindowSize != 8000 {
		t.Errorf("expected window size 8000, got %d", result.Budget.WindowSize)
	}
	if result.Budget.System == 0 {
		t.Error("expected non-zero system token count")
	}
	if result.Budget.History == 0 {
		t.Error("expected non-zero history token count")
	}
}

// TestIntegration_EmergencyCompaction tests that emergency compaction
// works as a last resort when context is too large.
func TestIntegration_EmergencyCompaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	estimator := ctxengine.NewCharEstimator(4.0)

	cfg := ctxengine.ContextConfig{
		EmergencyRetain: 2,
	}

	compactor := ctxengine.NewCompactor(nil, estimator, cfg)

	// Build a large history.
	var history []provider.LLMMessage
	for i := 0; i < 50; i++ {
		history = append(history, provider.LLMMessage{
			Role:    provider.MessageRoleUser,
			Content: "Some message content that takes up space in the context window.",
		})
	}

	// Emergency compact — should keep only 2 messages.
	result, err := compactor.EmergencyCompact(ctx, history)
	if err != nil {
		t.Fatalf("emergency compact: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 messages after emergency compaction, got %d", len(result))
	}

	// Should be the last 2 messages.
	if result[0].Content != history[48].Content {
		t.Error("expected the second-to-last message")
	}
	if result[1].Content != history[49].Content {
		t.Error("expected the last message")
	}
}

// TestIntegration_GracefulDegradation tests that the system works
// without a memory store and without a summarizer.
func TestIntegration_GracefulDegradation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	estimator := ctxengine.NewCharEstimator(4.0)

	// No memory store → InjectMemory returns nil.
	injected, err := memory.InjectMemory(ctx, memory.InjectionRequest{
		Store: nil, Query: "query", MaxFacts: 10, MaxTokens: 2000, Estimator: estimator,
	})
	if err != nil {
		t.Fatalf("inject with nil store: %v", err)
	}
	if injected != nil {
		t.Errorf("expected nil from nil store, got %v", injected)
	}

	// NopExtractor → Extract returns nil.
	nop := memory.NopExtractor{}
	facts, err := nop.Extract(ctx, memory.Exchange{})
	if err != nil {
		t.Fatalf("nop extract: %v", err)
	}
	if facts != nil {
		t.Errorf("expected nil from NopExtractor, got %v", facts)
	}

	// Compactor without summarizer → drops old messages.
	cfg := ctxengine.ContextConfig{
		CompactionThreshold: 3,
		RetainRecent:        2,
	}
	compactor := ctxengine.NewCompactor(nil, estimator, cfg)

	history := []provider.LLMMessage{
		{Role: provider.MessageRoleUser, Content: "msg1"},
		{Role: provider.MessageRoleAssistant, Content: "msg2"},
		{Role: provider.MessageRoleUser, Content: "msg3"},
		{Role: provider.MessageRoleAssistant, Content: "msg4"},
		{Role: provider.MessageRoleUser, Content: "msg5"},
	}

	result, err := compactor.Compact(ctx, history)
	if err != nil {
		t.Fatalf("compact without summarizer: %v", err)
	}

	// Without summarizer, should just keep the last 2.
	if len(result) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result))
	}
	if result[0].Content != "msg4" {
		t.Errorf("expected 'msg4', got %q", result[0].Content)
	}
}

// TestIntegration_HistoryStoreWithCompaction tests using HistoryStore
// to accumulate messages, then compact, then set summary.
func TestIntegration_HistoryStoreWithCompaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	estimator := ctxengine.NewCharEstimator(4.0)

	historyStore := memory.NewInMemoryHistoryStore()
	sessionID := "session-compact-1"

	// Accumulate messages.
	for i := 0; i < 10; i++ {
		role := provider.MessageRoleUser
		if i%2 == 1 {
			role = provider.MessageRoleAssistant
		}
		msg := provider.LLMMessage{
			Role:    role,
			Content: "Message number " + string(rune('0'+i)),
		}
		if err := historyStore.Append(sessionID, msg); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Get all messages.
	allMsgs, err := historyStore.GetAll(sessionID)
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(allMsgs) != 10 {
		t.Fatalf("expected 10 messages, got %d", len(allMsgs))
	}

	// Compact with a nil summarizer (drop old messages).
	cfg := ctxengine.ContextConfig{
		CompactionThreshold: 5,
		RetainRecent:        4,
	}
	compactor := ctxengine.NewCompactor(nil, estimator, cfg)
	compacted, err := compactor.Compact(ctx, allMsgs)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(compacted) != 4 {
		t.Errorf("expected 4 messages after compaction, got %d", len(compacted))
	}

	// Store a summary.
	if err := historyStore.SetSummary(sessionID, "User discussed numbered topics."); err != nil {
		t.Fatalf("set summary: %v", err)
	}

	summary, err := historyStore.GetSummary(sessionID)
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}
	if summary != "User discussed numbered topics." {
		t.Errorf("unexpected summary: %q", summary)
	}
}

// providerSummarizer adapts an integrationProvider to the Summarizer interface.
type providerSummarizer struct {
	provider *integrationProvider
}

func (s *providerSummarizer) Summarize(ctx context.Context, messages []provider.LLMMessage) (string, error) {
	resp, err := s.provider.Complete(ctx, provider.CompletionRequest{Messages: messages})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
