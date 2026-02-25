package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/provider/providertest"
	"github.com/flemzord/sclaw/pkg/message"
)

// noopAgentFactory is a minimal AgentFactory for router-level tests.
type noopAgentFactory struct{}

func (f *noopAgentFactory) ForSession(_ *Session, _ message.InboundMessage) (*agent.Loop, error) {
	mockProv := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			return provider.CompletionResponse{
				Content:      "ok",
				FinishReason: provider.FinishReasonStop,
			}, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test-model" },
	}
	return agent.NewLoop(mockProv, nil, agent.LoopConfig{}), nil
}

// blockingAgentFactory creates loops that block until context cancellation.
type blockingAgentFactory struct {
	started chan struct{}
	once    sync.Once
}

func (f *blockingAgentFactory) ForSession(_ *Session, _ message.InboundMessage) (*agent.Loop, error) {
	mockProv := &providertest.MockProvider{
		CompleteFunc: func(ctx context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			if f.started != nil {
				f.once.Do(func() { close(f.started) })
			}
			<-ctx.Done()
			return provider.CompletionResponse{}, ctx.Err()
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test-model" },
	}
	return agent.NewLoop(mockProv, nil, agent.LoopConfig{}), nil
}

// noopResponseSender records sent messages without side effects.
type noopResponseSender struct {
	mu   sync.Mutex
	sent []message.OutboundMessage
}

func (s *noopResponseSender) Send(_ context.Context, msg message.OutboundMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, msg)
	return nil
}

// newTestMessage creates a unique inbound message for router tests.
func newTestMessage(id string) message.InboundMessage {
	return message.InboundMessage{
		ID:      id,
		Channel: "slack",
		Sender:  message.Sender{ID: "user-1"},
		Chat:    message.Chat{ID: "C123", Type: message.ChatDM},
		Blocks:  []message.ContentBlock{message.NewTextBlock("hello")},
	}
}

func TestNewRouter_RequiresAgentFactory(t *testing.T) {
	t.Parallel()

	_, err := NewRouter(Config{
		ResponseSender: &noopResponseSender{},
		// AgentFactory is nil.
	})
	if !errors.Is(err, ErrNoAgentFactory) {
		t.Errorf("error = %v, want %v", err, ErrNoAgentFactory)
	}
}

func TestNewRouter_RequiresResponseSender(t *testing.T) {
	t.Parallel()

	_, err := NewRouter(Config{
		AgentFactory: &noopAgentFactory{},
		// ResponseSender is nil.
	})
	if !errors.Is(err, ErrNoResponseSender) {
		t.Errorf("error = %v, want %v", err, ErrNoResponseSender)
	}
}

func TestNewRouter_Defaults(t *testing.T) {
	t.Parallel()

	r, err := NewRouter(Config{
		AgentFactory:   &noopAgentFactory{},
		ResponseSender: &noopResponseSender{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if r.config.WorkerCount != DefaultWorkerCount {
		t.Errorf("WorkerCount = %d, want %d", r.config.WorkerCount, DefaultWorkerCount)
	}
	if r.config.InboxSize != defaultInboxSize {
		t.Errorf("InboxSize = %d, want %d", r.config.InboxSize, defaultInboxSize)
	}
	if r.config.MaxIdle != defaultMaxIdle {
		t.Errorf("MaxIdle = %v, want %v", r.config.MaxIdle, defaultMaxIdle)
	}
	if r.config.Logger == nil {
		t.Error("Logger should not be nil after defaults")
	}
}

func TestRouter_Submit_NonBlocking(t *testing.T) {
	t.Parallel()

	// Create router with inbox size 1 but do NOT start it.
	// This means no workers consume from the inbox, so it fills up.
	r, err := NewRouter(Config{
		InboxSize:      1,
		AgentFactory:   &noopAgentFactory{},
		ResponseSender: &noopResponseSender{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First submit should succeed — fills the inbox.
	if err := r.Submit(newTestMessage("msg-1")); err != nil {
		t.Fatalf("first Submit returned error: %v", err)
	}

	// Second submit should return ErrInboxFull immediately (non-blocking).
	err = r.Submit(newTestMessage("msg-2"))
	if !errors.Is(err, ErrInboxFull) {
		t.Errorf("second Submit error = %v, want %v", err, ErrInboxFull)
	}
}

func TestRouter_Submit_AfterStop(t *testing.T) {
	t.Parallel()

	r, err := NewRouter(Config{
		AgentFactory:   &noopAgentFactory{},
		ResponseSender: &noopResponseSender{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r.Start(context.Background())
	r.Stop(context.Background())

	// Submit after stop should return ErrRouterStopped.
	err = r.Submit(newTestMessage("msg-after-stop"))
	if !errors.Is(err, ErrRouterStopped) {
		t.Errorf("Submit after Stop error = %v, want %v", err, ErrRouterStopped)
	}
}

func TestRouter_GracefulShutdown(t *testing.T) {
	t.Parallel()

	sender := &noopResponseSender{}
	r, err := NewRouter(Config{
		WorkerCount:    2,
		AgentFactory:   &noopAgentFactory{},
		ResponseSender: sender,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r.Start(context.Background())

	// Submit a few messages.
	for i := 0; i < 3; i++ {
		if err := r.Submit(newTestMessage("msg-" + string(rune('a'+i)))); err != nil {
			t.Fatalf("Submit(%d) error: %v", i, err)
		}
	}

	// Stop should complete without hanging.
	done := make(chan struct{})
	go func() {
		r.Stop(context.Background())
		close(done)
	}()

	select {
	case <-done:
		// Success — stop completed.
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not complete within 5 seconds")
	}
}

func TestRouter_EndToEnd(t *testing.T) {
	t.Parallel()

	sender := &noopResponseSender{}
	r, err := NewRouter(Config{
		WorkerCount:    2,
		InboxSize:      10,
		AgentFactory:   &noopAgentFactory{},
		ResponseSender: sender,
		GroupPolicy:    GroupPolicy{Mode: GroupPolicyAllowAll},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r.Start(context.Background())

	// Submit a message and wait for it to be processed.
	if err := r.Submit(newTestMessage("e2e-msg")); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	// Give workers time to process.
	deadline := time.After(5 * time.Second)
	for {
		sender.mu.Lock()
		count := len(sender.sent)
		sender.mu.Unlock()
		if count >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for response to be sent")
		case <-time.After(10 * time.Millisecond):
			// Retry.
		}
	}

	r.Stop(context.Background())

	// Verify response was sent.
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.sent) != 1 {
		t.Errorf("sent %d messages, want 1", len(sender.sent))
	}
}

func TestRouter_PruneSessions(t *testing.T) {
	t.Parallel()

	r, err := NewRouter(Config{
		AgentFactory:   &noopAgentFactory{},
		ResponseSender: &noopResponseSender{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// PruneSessions should not panic on an empty store.
	pruned := r.PruneSessions()
	if pruned != 0 {
		t.Errorf("PruneSessions() = %d, want 0 on empty store", pruned)
	}
}

func TestRouter_Stop_Idempotent(t *testing.T) {
	t.Parallel()

	r, err := NewRouter(Config{
		AgentFactory:   &noopAgentFactory{},
		ResponseSender: &noopResponseSender{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r.Start(context.Background())
	r.Stop(context.Background())
	r.Stop(context.Background())
}

func TestRouter_SubmitConcurrentWithStop_NoPanic(t *testing.T) {
	t.Parallel()

	r, err := NewRouter(Config{
		WorkerCount:    2,
		InboxSize:      32,
		AgentFactory:   &noopAgentFactory{},
		ResponseSender: &noopResponseSender{},
		GroupPolicy:    GroupPolicy{Mode: GroupPolicyAllowAll},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r.Start(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = r.Submit(newTestMessage(fmt.Sprintf("msg-%d-%d", worker, j)))
			}
		}(i)
	}

	time.Sleep(10 * time.Millisecond)
	r.Stop(context.Background())
	wg.Wait()
}

func TestRouter_Stop_CancelsInFlightHandler(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	r, err := NewRouter(Config{
		WorkerCount:    1,
		InboxSize:      1,
		AgentFactory:   &blockingAgentFactory{started: started},
		ResponseSender: &noopResponseSender{},
		GroupPolicy:    GroupPolicy{Mode: GroupPolicyAllowAll},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r.Start(context.Background())
	if err := r.Submit(newTestMessage("blocking-msg")); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for in-flight handler start")
	}

	done := make(chan struct{})
	go func() {
		r.Stop(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not complete; expected cancellation of in-flight handler")
	}
}

func TestRouter_Submit_UnmatchedApprovalFallsBackToPipeline(t *testing.T) {
	t.Parallel()

	sender := &noopResponseSender{}
	r, err := NewRouter(Config{
		WorkerCount:    1,
		InboxSize:      8,
		AgentFactory:   &noopAgentFactory{},
		ResponseSender: sender,
		GroupPolicy:    GroupPolicy{Mode: GroupPolicyAllowAll},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r.Start(context.Background())
	defer r.Stop(context.Background())

	msg := newTestMessage("approval-like-msg")
	msg.Blocks = []message.ContentBlock{message.NewTextBlock("approve unknown-id")}
	if err := r.Submit(msg); err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		sender.mu.Lock()
		count := len(sender.sent)
		sender.mu.Unlock()
		if count >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for message to be processed through pipeline")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
