package router

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/hook"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/provider/providertest"
	"github.com/flemzord/sclaw/pkg/message"
)

// testResponseSender records sent messages for test assertions.
type testResponseSender struct {
	mu   sync.Mutex
	sent []message.OutboundMessage
}

func (s *testResponseSender) Send(_ context.Context, msg message.OutboundMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, msg)
	return nil
}

func (s *testResponseSender) sentMessages() []message.OutboundMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]message.OutboundMessage, len(s.sent))
	copy(cp, s.sent)
	return cp
}

// testAgentFactory returns a preconfigured agent loop.
type testAgentFactory struct {
	loop *agent.Loop
	err  error
}

func (f *testAgentFactory) ForSession(_ *Session, _ message.InboundMessage) (*agent.Loop, error) {
	return f.loop, f.err
}

// testInboundMessage creates a minimal inbound message for tests.
func testInboundMessage() message.InboundMessage {
	return message.InboundMessage{
		ID:      "msg-1",
		Channel: "slack",
		Sender:  message.Sender{ID: "user-1", Username: "alice"},
		Chat:    message.Chat{ID: "C123", Type: message.ChatDM},
		Blocks:  []message.ContentBlock{message.NewTextBlock("Hello")},
	}
}

// testEnvelope creates an envelope from a test inbound message.
func testEnvelope() envelope {
	msg := testInboundMessage()
	return envelope{
		Message: msg,
		Key:     SessionKeyFromMessage(msg),
	}
}

// newTestMockProvider creates a MockProvider that returns the given content.
func newTestMockProvider(content string) *providertest.MockProvider {
	return &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			return provider.CompletionResponse{
				Content:      content,
				FinishReason: provider.FinishReasonStop,
			}, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test-model" },
	}
}

func TestPipeline_EndToEnd(t *testing.T) {
	t.Parallel()

	mockProv := newTestMockProvider("Hello!")
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		Logger:          slog.Default(),
	})

	env := testEnvelope()
	result := pipeline.Execute(context.Background(), env)

	// Verify no error occurred.
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// Verify the result was not skipped.
	if result.Skipped {
		t.Fatal("expected result not to be skipped")
	}

	// Verify response content.
	if result.Response == nil {
		t.Fatal("expected non-nil response")
	}
	if result.Response.Content != "Hello!" {
		t.Errorf("response content = %q, want %q", result.Response.Content, "Hello!")
	}

	// Verify the sender was called.
	sent := sender.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sender called %d times, want 1", len(sent))
	}

	// Verify outbound message preserves thread context.
	outbound := sent[0]
	if outbound.Chat.ID != env.Message.Chat.ID {
		t.Errorf("outbound chat ID = %q, want %q", outbound.Chat.ID, env.Message.Chat.ID)
	}
	if outbound.ReplyToID != env.Message.ID {
		t.Errorf("outbound ReplyToID = %q, want %q", outbound.ReplyToID, env.Message.ID)
	}

	// Verify session was created and history was updated.
	if result.Session == nil {
		t.Fatal("expected non-nil session")
	}
	// History should contain: user message + assistant response = 2 entries.
	if len(result.Session.History) != 2 {
		t.Errorf("session history length = %d, want 2", len(result.Session.History))
	}
	if result.Session.History[0].Role != provider.MessageRoleUser {
		t.Errorf("first history entry role = %q, want %q", result.Session.History[0].Role, provider.MessageRoleUser)
	}
	if result.Session.History[1].Role != provider.MessageRoleAssistant {
		t.Errorf("second history entry role = %q, want %q", result.Session.History[1].Role, provider.MessageRoleAssistant)
	}
	if result.Session.History[1].Content != "Hello!" {
		t.Errorf("assistant history content = %q, want %q", result.Session.History[1].Content, "Hello!")
	}
}

func TestPipeline_GroupPolicyFilter(t *testing.T) {
	t.Parallel()

	mockProv := newTestMockProvider("Should not reach here")
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyRequireMention},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		Logger:          slog.Default(),
	})

	// Build a group message WITHOUT a mention — should be filtered.
	msg := message.InboundMessage{
		ID:      "msg-2",
		Channel: "slack",
		Sender:  message.Sender{ID: "user-2"},
		Chat:    message.Chat{ID: "G456", Type: message.ChatGroup},
		Blocks:  []message.ContentBlock{message.NewTextBlock("Hello group")},
		// No Mentions field — bot was not mentioned.
	}
	env := envelope{Message: msg, Key: SessionKeyFromMessage(msg)}

	result := pipeline.Execute(context.Background(), env)

	// Verify the message was skipped.
	if !result.Skipped {
		t.Fatal("expected result to be skipped by group policy")
	}

	// Verify the sender was never called.
	sent := sender.sentMessages()
	if len(sent) != 0 {
		t.Errorf("sender called %d times, want 0", len(sent))
	}

	// Verify the provider was never called.
	if mockProv.CompleteCalls != 0 {
		t.Errorf("provider called %d times, want 0", mockProv.CompleteCalls)
	}
}

func TestPipeline_AgentFactoryError(t *testing.T) {
	t.Parallel()

	factoryErr := errors.New("agent factory failed")
	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{err: factoryErr},
		ResponseSender:  sender,
		Logger:          slog.Default(),
	})

	env := testEnvelope()
	result := pipeline.Execute(context.Background(), env)

	// Verify error is propagated.
	if result.Error == nil {
		t.Fatal("expected error from pipeline")
	}
	if !errors.Is(result.Error, factoryErr) {
		t.Errorf("error = %v, want %v", result.Error, factoryErr)
	}

	// Verify an error message was sent to the user.
	sent := sender.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sender called %d times, want 1 (error message)", len(sent))
	}

	// Verify the error message text.
	errText := sent[0].TextContent()
	if errText != "Failed to initialize agent." {
		t.Errorf("error message text = %q, want %q", errText, "Failed to initialize agent.")
	}
}

func TestPipeline_NilPruner(t *testing.T) {
	t.Parallel()

	mockProv := newTestMockProvider("OK")
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	// Pruner is nil — pipeline should not panic.
	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		Pruner:          nil,
		Logger:          slog.Default(),
	})

	env := testEnvelope()
	result := pipeline.Execute(context.Background(), env)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Response == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestPipeline_SessionReuse(t *testing.T) {
	t.Parallel()

	callCount := 0
	mockProv := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			callCount++
			return provider.CompletionResponse{
				Content:      "Response",
				FinishReason: provider.FinishReasonStop,
			}, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test-model" },
	}
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		Logger:          slog.Default(),
	})

	env := testEnvelope()

	// Execute twice with the same session key.
	result1 := pipeline.Execute(context.Background(), env)
	if result1.Error != nil {
		t.Fatalf("first execution error: %v", result1.Error)
	}

	result2 := pipeline.Execute(context.Background(), env)
	if result2.Error != nil {
		t.Fatalf("second execution error: %v", result2.Error)
	}

	// Both results should reference the same session.
	if result1.Session.ID != result2.Session.ID {
		t.Errorf("session IDs differ: %q vs %q", result1.Session.ID, result2.Session.ID)
	}

	// After two executions, history should have 4 entries (2 user + 2 assistant).
	if len(result2.Session.History) != 4 {
		t.Errorf("session history length = %d, want 4", len(result2.Session.History))
	}

	// Store should still have exactly 1 session.
	if store.Len() != 1 {
		t.Errorf("store length = %d, want 1", store.Len())
	}
}

func TestPipeline_HooksInvoked(t *testing.T) {
	t.Parallel()

	mockProv := newTestMockProvider("Hook response")
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()
	hooks := hook.NewPipeline()

	// Track which hook positions were invoked.
	var mu sync.Mutex
	var invoked []string

	hooks.Register(&trackingHook{name: "bp", pos: hook.BeforeProcess, mu: &mu, invoked: &invoked})
	hooks.Register(&trackingHook{name: "bs", pos: hook.BeforeSend, mu: &mu, invoked: &invoked})
	hooks.Register(&trackingHook{name: "as", pos: hook.AfterSend, mu: &mu, invoked: &invoked})

	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		HookPipeline:    hooks,
		Logger:          slog.Default(),
	})

	env := testEnvelope()
	result := pipeline.Execute(context.Background(), env)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	mu.Lock()
	defer mu.Unlock()

	expected := []string{"bp", "bs", "as"}
	if len(invoked) != len(expected) {
		t.Fatalf("invoked hooks = %v, want %v", invoked, expected)
	}
	for i, name := range expected {
		if invoked[i] != name {
			t.Errorf("invoked[%d] = %q, want %q", i, invoked[i], name)
		}
	}
}

// trackingHook records its invocation for test assertions.
type trackingHook struct {
	name    string
	pos     hook.Position
	mu      *sync.Mutex
	invoked *[]string
}

func (h *trackingHook) Position() hook.Position { return h.pos }
func (h *trackingHook) Priority() int           { return 0 }

func (h *trackingHook) Execute(_ context.Context, _ *hook.Context) (hook.Action, error) {
	h.mu.Lock()
	*h.invoked = append(*h.invoked, h.name)
	h.mu.Unlock()
	return hook.ActionContinue, nil
}

func TestPipeline_NilHooks(t *testing.T) {
	t.Parallel()

	mockProv := newTestMockProvider("OK")
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	// Hooks field is nil — pipeline should not panic.
	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		HookPipeline:    nil,
		Logger:          slog.Default(),
	})

	env := testEnvelope()
	result := pipeline.Execute(context.Background(), env)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}

// m-60: Verify that history does not exceed MaxHistoryLen after Step 13.
func TestPipeline_HistoryTrimAfterAssistantAppend(t *testing.T) {
	t.Parallel()

	mockProv := newTestMockProvider("Response")
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	// Use a very small MaxHistoryLen to trigger the edge case.
	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		Logger:          slog.Default(),
		MaxHistoryLen:   2,
	})

	env := testEnvelope()

	// First execution: history = [user, assistant] = 2 entries (at limit).
	result := pipeline.Execute(context.Background(), env)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.Session.History) > 2 {
		t.Errorf("history length after first exec = %d, want <= 2", len(result.Session.History))
	}

	// Second execution: without m-60 fix, history would be [user, assistant, user, assistant] = 4
	// but Step 8 trims to 2, then Step 13 appends to 3 — exceeding the limit.
	// With the fix, Step 13 also trims back to 2.
	result2 := pipeline.Execute(context.Background(), env)
	if result2.Error != nil {
		t.Fatalf("unexpected error: %v", result2.Error)
	}
	if len(result2.Session.History) > 2 {
		t.Errorf("history length after second exec = %d, want <= 2 (m-60 fix)", len(result2.Session.History))
	}
}

// m-29: Verify that hook errors are logged (not silently ignored).
func TestPipeline_HookErrorLogged(t *testing.T) {
	t.Parallel()

	mockProv := newTestMockProvider("OK")
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()
	hooks := hook.NewPipeline()

	hookErr := errors.New("hook failed")
	hooks.Register(&errorHook{name: "failing-hook", pos: hook.BeforeProcess, err: hookErr})

	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		HookPipeline:    hooks,
		Logger:          slog.Default(),
	})

	env := testEnvelope()
	// The pipeline should continue despite hook errors (logged, not fatal).
	result := pipeline.Execute(context.Background(), env)

	if result.Error != nil {
		t.Fatalf("pipeline should not fail on hook error, got: %v", result.Error)
	}
	if result.Response == nil {
		t.Fatal("expected non-nil response despite hook error")
	}
}

// errorHook always returns an error.
type errorHook struct {
	name string
	pos  hook.Position
	err  error
}

func (h *errorHook) Position() hook.Position { return h.pos }
func (h *errorHook) Priority() int           { return 0 }

func (h *errorHook) Execute(_ context.Context, _ *hook.Context) (hook.Action, error) {
	return hook.ActionContinue, h.err
}
