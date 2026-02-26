package router

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/channel/channeltest"
	"github.com/flemzord/sclaw/internal/hook"
	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/provider/providertest"
	"github.com/flemzord/sclaw/pkg/message"
)

// testHistoryResolver is a simple in-test mock for HistoryResolver.
type testHistoryResolver struct {
	store memory.HistoryStore
}

func (r *testHistoryResolver) ResolveHistory(_ string) memory.HistoryStore {
	return r.store
}

// inMemoryHistoryStore implements memory.HistoryStore for testing with a simple map.
type inMemoryHistoryStore struct {
	mu       sync.Mutex
	messages map[string][]provider.LLMMessage
}

func newInMemoryHistoryStore() *inMemoryHistoryStore {
	return &inMemoryHistoryStore{messages: make(map[string][]provider.LLMMessage)}
}

func (s *inMemoryHistoryStore) Append(sessionID string, msg provider.LLMMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[sessionID] = append(s.messages[sessionID], msg)
	return nil
}

func (s *inMemoryHistoryStore) GetRecent(sessionID string, n int) ([]provider.LLMMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.messages[sessionID]
	if len(msgs) <= n {
		cp := make([]provider.LLMMessage, len(msgs))
		copy(cp, msgs)
		return cp, nil
	}
	cp := make([]provider.LLMMessage, n)
	copy(cp, msgs[len(msgs)-n:])
	return cp, nil
}

func (s *inMemoryHistoryStore) GetAll(sessionID string) ([]provider.LLMMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]provider.LLMMessage, len(s.messages[sessionID]))
	copy(cp, s.messages[sessionID])
	return cp, nil
}

func (s *inMemoryHistoryStore) SetSummary(string, string) error   { return nil }
func (s *inMemoryHistoryStore) GetSummary(string) (string, error) { return "", nil }
func (s *inMemoryHistoryStore) Purge(string) error                { return nil }
func (s *inMemoryHistoryStore) Len(sessionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages[sessionID]), nil
}

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

// testChannelLookup wraps a map of channels to implement ChannelLookup.
type testChannelLookup struct {
	channels map[string]channel.Channel
}

func (l *testChannelLookup) Get(name string) (channel.Channel, bool) {
	ch, ok := l.channels[name]
	return ch, ok
}

func TestPipeline_TypingIndicator(t *testing.T) {
	t.Parallel()

	// Use a slow provider so the typing goroutine has time to send
	// at least one indicator before loop.Run returns.
	mockProv := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			time.Sleep(50 * time.Millisecond)
			return provider.CompletionResponse{
				Content:      "Typed!",
				FinishReason: provider.FinishReasonStop,
			}, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test-model" },
	}
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	// Create a mock channel that supports typing.
	mockCh := channeltest.NewMockStreamingChannel("slack", nil)
	lookup := &testChannelLookup{
		channels: map[string]channel.Channel{"slack": mockCh},
	}

	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		ChannelLookup:   lookup,
		Logger:          slog.Default(),
	})

	env := testEnvelope()
	result := pipeline.Execute(context.Background(), env)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// Verify that SendTyping was called at least once.
	chats := mockCh.TypingChats()
	if len(chats) == 0 {
		t.Fatal("expected at least one typing indicator, got none")
	}

	// Verify the typing was sent to the correct chat.
	if chats[0].ID != env.Message.Chat.ID {
		t.Errorf("typing chat ID = %q, want %q", chats[0].ID, env.Message.Chat.ID)
	}
}

func TestPipeline_TypingIndicator_NonTypingChannel(t *testing.T) {
	t.Parallel()

	mockProv := newTestMockProvider("No typing")
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	// Use a plain MockChannel that does NOT implement TypingChannel.
	mockCh := channeltest.NewMockChannel("slack", nil)
	lookup := &testChannelLookup{
		channels: map[string]channel.Channel{"slack": mockCh},
	}

	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		ChannelLookup:   lookup,
		Logger:          slog.Default(),
	})

	env := testEnvelope()
	result := pipeline.Execute(context.Background(), env)

	// Should succeed without error — just no typing indicator sent.
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Response == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestPipeline_HistoryPersistAndRestore(t *testing.T) {
	t.Parallel()

	mockProv := newTestMockProvider("Persisted!")
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	histStore := newInMemoryHistoryStore()
	resolver := &testHistoryResolver{store: histStore}

	// Factory that also sets the AgentID on the session.
	factory := &testAgentFactory{loop: loop}
	agentSettingFactory := &agentIDSettingFactory{
		inner:   factory,
		agentID: "assistant",
	}

	sessionStore := NewInMemorySessionStore()
	pipeline := NewPipeline(PipelineConfig{
		Store:           sessionStore,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    agentSettingFactory,
		ResponseSender:  sender,
		Logger:          slog.Default(),
		HistoryResolver: resolver,
	})

	env := testEnvelope()

	// Execute first message.
	result := pipeline.Execute(context.Background(), env)
	if result.Error != nil {
		t.Fatalf("first execution error: %v", result.Error)
	}

	// Verify messages were persisted.
	pKey := persistenceKey(env.Key)
	persisted, err := histStore.GetAll(pKey)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(persisted) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(persisted))
	}
	if persisted[0].Role != provider.MessageRoleUser {
		t.Errorf("persisted[0].Role = %q, want user", persisted[0].Role)
	}
	if persisted[1].Role != provider.MessageRoleAssistant {
		t.Errorf("persisted[1].Role = %q, want assistant", persisted[1].Role)
	}

	// Simulate a restart: delete session from store, then send another message.
	// The session should be recreated and history should be restored from SQLite.
	sessionStore.Delete(env.Key)

	result2 := pipeline.Execute(context.Background(), env)
	if result2.Error != nil {
		t.Fatalf("second execution error: %v", result2.Error)
	}

	// After restore (2 msgs) + new user + new assistant = 4 in session.History.
	if len(result2.Session.History) != 4 {
		t.Errorf("session history after restore = %d, want 4", len(result2.Session.History))
	}

	// Verify the first entry is the restored user message.
	if result2.Session.History[0].Role != provider.MessageRoleUser {
		t.Errorf("restored[0].Role = %q, want user", result2.Session.History[0].Role)
	}
}

// agentIDSettingFactory wraps a factory and sets AgentID on the session.
type agentIDSettingFactory struct {
	inner   AgentFactory
	agentID string
}

func (f *agentIDSettingFactory) ForSession(session *Session, msg message.InboundMessage) (*agent.Loop, error) {
	if session.AgentID == "" {
		session.AgentID = f.agentID
	}
	return f.inner.ForSession(session, msg)
}

func TestPipeline_HistoryResolver_Nil(t *testing.T) {
	t.Parallel()

	mockProv := newTestMockProvider("OK")
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	// HistoryResolver is nil — pipeline should not panic (backward compat).
	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		Logger:          slog.Default(),
		HistoryResolver: nil,
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
