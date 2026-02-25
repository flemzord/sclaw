package router

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/provider/providertest"
	"github.com/flemzord/sclaw/internal/workspace"
	"github.com/flemzord/sclaw/internal/workspace/workspacetest"
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

func (f *testAgentFactory) ForSession(_ *Session) (*agent.Loop, error) {
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

func TestPipeline_WithSoulAndSkills(t *testing.T) {
	t.Parallel()

	// Capture the messages sent to the provider to extract the system prompt.
	var capturedMessages []provider.LLMMessage
	mockProv := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
			capturedMessages = req.Messages
			return provider.CompletionResponse{
				Content:      "Aye!",
				FinishReason: provider.FinishReasonStop,
			}, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test-model" },
	}
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	soulProvider := &workspacetest.MockSoulProvider{
		LoadFunc: func() (string, error) {
			return "You are a pirate captain.", nil
		},
	}

	skills := []workspace.Skill{
		{
			Meta: workspace.SkillMeta{
				Name:          "nav",
				Description:   "Navigation",
				ToolsRequired: []string{"compass"},
				Trigger:       workspace.TriggerAlways,
			},
			Body: "Navigate the seas.",
		},
		{
			Meta: workspace.SkillMeta{
				Name:          "review",
				Description:   "Code review",
				ToolsRequired: []string{"read_file"},
				Trigger:       workspace.TriggerAuto,
				Keywords:      []string{"review"},
			},
			Body: "Review the code.",
		},
	}

	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        NewLaneLock(),
		GroupPolicy:     GroupPolicy{Mode: GroupPolicyAllowAll},
		ApprovalManager: NewApprovalManager(),
		AgentFactory:    &testAgentFactory{loop: loop},
		ResponseSender:  sender,
		Logger:          slog.Default(),
		SoulProvider:    soulProvider,
		SkillActivator:  workspace.NewSkillActivator(),
		Skills:          skills,
	})

	env := testEnvelope()
	result := pipeline.Execute(context.Background(), env)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// The agent loop injects the system prompt as the first message with role "system".
	if len(capturedMessages) == 0 {
		t.Fatal("expected captured messages")
	}
	systemMsg := capturedMessages[0]
	if systemMsg.Role != provider.MessageRoleSystem {
		t.Fatalf("first message role = %q, want %q", systemMsg.Role, provider.MessageRoleSystem)
	}

	// Verify the system prompt contains the SOUL content.
	if !strings.Contains(systemMsg.Content, "You are a pirate captain.") {
		t.Errorf("system prompt missing SOUL content, got: %q", systemMsg.Content)
	}

	// The "nav" skill requires "compass" which is not in AvailableTools (empty),
	// so it should NOT be activated. The "review" skill requires keyword match.
	// With the default test message ("Hello"), neither skill should activate.
	if strings.Contains(systemMsg.Content, "Active Skills") {
		t.Errorf("system prompt should not contain skills (no tools available), got: %q", systemMsg.Content)
	}
}

func TestPipeline_NilSoulProvider_DefaultPrompt(t *testing.T) {
	t.Parallel()

	var capturedMessages []provider.LLMMessage
	mockProv := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
			capturedMessages = req.Messages
			return provider.CompletionResponse{
				Content:      "OK",
				FinishReason: provider.FinishReasonStop,
			}, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test-model" },
	}
	loop := agent.NewLoop(mockProv, nil, agent.LoopConfig{})

	sender := &testResponseSender{}
	store := NewInMemorySessionStore()

	// No SoulProvider, no skills — should use DefaultSoulPrompt.
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

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// The first message should be the system prompt with the default value.
	if len(capturedMessages) == 0 {
		t.Fatal("expected captured messages")
	}
	systemMsg := capturedMessages[0]
	if systemMsg.Role != provider.MessageRoleSystem {
		t.Fatalf("first message role = %q, want %q", systemMsg.Role, provider.MessageRoleSystem)
	}
	if systemMsg.Content != workspace.DefaultSoulPrompt {
		t.Errorf("system prompt = %q, want %q", systemMsg.Content, workspace.DefaultSoulPrompt)
	}
}
