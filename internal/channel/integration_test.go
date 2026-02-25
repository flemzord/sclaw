package channel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/channel/channeltest"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/provider/providertest"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/pkg/message"
)

// echoAgentFactory returns an agent loop that echoes back the last user message.
type echoAgentFactory struct{}

func (f *echoAgentFactory) ForSession(_ *router.Session, _ message.InboundMessage) (*agent.Loop, error) {
	mock := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
			// Echo the last user message.
			var lastUserContent string
			for i := len(req.Messages) - 1; i >= 0; i-- {
				if req.Messages[i].Role == provider.MessageRoleUser {
					lastUserContent = req.Messages[i].Content
					break
				}
			}
			return provider.CompletionResponse{
				Content:      "echo: " + lastUserContent,
				FinishReason: provider.FinishReasonStop,
			}, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "echo-model" },
	}
	return agent.NewLoop(mock, nil, agent.LoopConfig{}), nil
}

// TestEndToEnd_MockChannelThroughRouter verifies the full flow:
// MockChannel -> Router.Submit -> Pipeline -> Agent Loop -> Dispatcher -> MockChannel.SentMessages
func TestEndToEnd_MockChannelThroughRouter(t *testing.T) {
	t.Parallel()

	// 1. Create a mock channel with an allow-list.
	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := channeltest.NewMockChannel("test", al)

	// 2. Create a dispatcher and register the channel.
	dispatcher := channel.NewDispatcher()
	if err := dispatcher.Register("test", ch); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// 3. Create the router with the dispatcher as ResponseSender.
	r, err := router.NewRouter(router.Config{
		WorkerCount:    2,
		InboxSize:      16,
		AgentFactory:   &echoAgentFactory{},
		ResponseSender: dispatcher,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	// 4. Wire the channel's inbox to the router.
	ch.SetInbox(r.Submit)

	// 5. Start the router.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.Start(ctx)
	defer r.Stop(ctx)

	// 6. Simulate an inbound message from alice.
	inMsg := message.InboundMessage{
		ID:     "msg-1",
		Sender: message.Sender{ID: "alice"},
		Chat:   message.Chat{ID: "chat-1", Type: message.ChatDM},
		Blocks: []message.ContentBlock{message.NewTextBlock("hello world")},
	}

	if err := ch.SimulateMessage(inMsg); err != nil {
		t.Fatalf("SimulateMessage: %v", err)
	}

	// 7. Wait for the response to appear.
	deadline := time.After(3 * time.Second)
	for {
		sent := ch.SentMessages()
		if len(sent) > 0 {
			// Verify the response content.
			got := sent[0].TextContent()
			want := "echo: hello world"
			if got != want {
				t.Errorf("response = %q, want %q", got, want)
			}
			// Verify the response targets the correct channel.
			if sent[0].Channel != "test" {
				t.Errorf("response Channel = %q, want %q", sent[0].Channel, "test")
			}
			// Verify thread/reply context is preserved.
			if sent[0].ReplyToID != "msg-1" {
				t.Errorf("response ReplyToID = %q, want %q", sent[0].ReplyToID, "msg-1")
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for response")
		case <-time.After(10 * time.Millisecond):
			// Poll again.
		}
	}
}

// TestEndToEnd_DeniedUserGetsNoResponse verifies that an unauthorized user
// is blocked by the allow-list and never reaches the router.
func TestEndToEnd_DeniedUserGetsNoResponse(t *testing.T) {
	t.Parallel()

	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := channeltest.NewMockChannel("test", al)

	// Try to simulate a message from bob (not in allow-list).
	msg := message.InboundMessage{
		ID:     "msg-denied",
		Sender: message.Sender{ID: "bob"},
		Chat:   message.Chat{ID: "chat-1", Type: message.ChatDM},
		Blocks: []message.ContentBlock{message.NewTextBlock("sneaky")},
	}

	err := ch.SimulateMessage(msg)
	if err == nil {
		t.Fatal("SimulateMessage should have returned an error for denied user")
	}
	if !errors.Is(err, channel.ErrDenied) {
		t.Errorf("error = %v, want ErrDenied", err)
	}
}

// TestEndToEnd_NoAllowListDeniesEveryone verifies that a channel without
// an allow-list denies all messages.
func TestEndToEnd_NoAllowListDeniesEveryone(t *testing.T) {
	t.Parallel()

	ch := channeltest.NewMockChannel("test", nil)

	msg := message.InboundMessage{
		ID:     "msg-1",
		Sender: message.Sender{ID: "alice"},
		Chat:   message.Chat{ID: "chat-1", Type: message.ChatDM},
		Blocks: []message.ContentBlock{message.NewTextBlock("hello")},
	}

	err := ch.SimulateMessage(msg)
	if !errors.Is(err, channel.ErrDenied) {
		t.Errorf("error = %v, want ErrDenied", err)
	}
}

// TestEndToEnd_MultipleChannels verifies that the dispatcher routes
// responses to the correct channel.
func TestEndToEnd_MultipleChannels(t *testing.T) {
	t.Parallel()

	al := channel.NewAllowList([]string{"alice"}, nil)
	ch1 := channeltest.NewMockChannel("slack", al)
	ch2 := channeltest.NewMockChannel("telegram", al)

	dispatcher := channel.NewDispatcher()
	_ = dispatcher.Register("slack", ch1)
	_ = dispatcher.Register("telegram", ch2)

	r, err := router.NewRouter(router.Config{
		WorkerCount:    2,
		InboxSize:      16,
		AgentFactory:   &echoAgentFactory{},
		ResponseSender: dispatcher,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	ch1.SetInbox(r.Submit)
	ch2.SetInbox(r.Submit)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.Start(ctx)
	defer r.Stop(ctx)

	// Send a message through the "slack" channel.
	slackMsg := message.InboundMessage{
		ID:     "slack-msg-1",
		Sender: message.Sender{ID: "alice"},
		Chat:   message.Chat{ID: "chat-slack", Type: message.ChatDM},
		Blocks: []message.ContentBlock{message.NewTextBlock("from slack")},
	}
	if err := ch1.SimulateMessage(slackMsg); err != nil {
		t.Fatalf("SimulateMessage(slack): %v", err)
	}

	// Wait for the response on ch1.
	deadline := time.After(3 * time.Second)
	for len(ch1.SentMessages()) == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for slack response")
		case <-time.After(10 * time.Millisecond):
		}
	}

	sent := ch1.SentMessages()
	if sent[0].Channel != "slack" {
		t.Errorf("slack response Channel = %q, want %q", sent[0].Channel, "slack")
	}

	// ch2 should not have received any messages.
	if len(ch2.SentMessages()) != 0 {
		t.Errorf("telegram channel received %d unexpected messages", len(ch2.SentMessages()))
	}
}

// TestEndToEnd_TypingIndicator verifies that StartTypingLoop sends
// indicators and stops when context is cancelled.
func TestEndToEnd_TypingIndicator(t *testing.T) {
	t.Parallel()

	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := channeltest.NewMockStreamingChannel("test", al)
	chat := message.Chat{ID: "chat-1", Type: message.ChatDM}

	ctx, cancel := context.WithCancel(context.Background())

	// Start typing with a short interval.
	channel.StartTypingLoop(ctx, ch, chat, 20*time.Millisecond)

	// Let it tick a few times.
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Give the goroutine time to exit.
	time.Sleep(20 * time.Millisecond)

	chats := ch.TypingChats()
	if len(chats) < 2 {
		t.Errorf("expected at least 2 typing indicators, got %d", len(chats))
	}
}

// TestEndToEnd_StreamingChannel verifies that SendStream collects chunks.
func TestEndToEnd_StreamingChannel(t *testing.T) {
	t.Parallel()

	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := channeltest.NewMockStreamingChannel("test", al)

	if !ch.SupportsStreaming() {
		t.Fatal("mock streaming channel should support streaming")
	}

	chat := message.Chat{ID: "chat-1", Type: message.ChatDM}
	stream := make(chan string, 3)
	stream <- "Hello "
	stream <- "World"
	stream <- "!"
	close(stream)

	if err := ch.SendStream(context.Background(), chat, stream); err != nil {
		t.Fatalf("SendStream: %v", err)
	}

	chunks := ch.StreamChunks()
	if len(chunks) != 3 {
		t.Fatalf("expected 3 stream chunks, got %d", len(chunks))
	}
	if chunks[0] != "Hello " || chunks[1] != "World" || chunks[2] != "!" {
		t.Errorf("unexpected chunks: %q", chunks)
	}
}
