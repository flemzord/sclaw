package channeltest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/pkg/message"
)

func TestMockChannel_ModuleInfo(t *testing.T) {
	t.Parallel()
	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := NewMockChannel("telegram", al)
	info := ch.ModuleInfo()

	if string(info.ID) != "channel.telegram" {
		t.Errorf("ModuleID = %q, want %q", info.ID, "channel.telegram")
	}
	if info.New == nil {
		t.Fatal("New func should not be nil")
	}
	inst := info.New()
	if inst == nil {
		t.Fatal("New() returned nil")
	}
}

func TestMockChannel_SendRecords(t *testing.T) {
	t.Parallel()
	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := NewMockChannel("test", al)
	msg := message.OutboundMessage{
		Channel: "test",
		Chat:    message.Chat{ID: "chat-1"},
		Blocks:  []message.ContentBlock{message.NewTextBlock("hello")},
	}

	if err := ch.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent := ch.SentMessages()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sent))
	}
	if sent[0].Blocks[0].Text != "hello" {
		t.Errorf("sent text = %q, want %q", sent[0].Blocks[0].Text, "hello")
	}
}

func TestMockChannel_SetInboxAndSimulate(t *testing.T) {
	t.Parallel()
	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := NewMockChannel("test", al)

	var received message.InboundMessage
	ch.SetInbox(func(msg message.InboundMessage) error {
		received = msg
		return nil
	})

	msg := message.InboundMessage{
		Sender: message.Sender{ID: "alice"},
		Chat:   message.Chat{ID: "chat-1", Type: message.ChatDM},
	}
	if err := ch.SimulateMessage(msg); err != nil {
		t.Fatalf("SimulateMessage: %v", err)
	}

	if received.Channel != "test" {
		t.Errorf("Channel = %q, want %q", received.Channel, "test")
	}
	if received.Sender.ID != "alice" {
		t.Errorf("Sender.ID = %q, want %q", received.Sender.ID, "alice")
	}
}

func TestMockChannel_SimulateDeniedByAllowList(t *testing.T) {
	t.Parallel()
	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := NewMockChannel("test", al)

	msg := message.InboundMessage{
		Sender: message.Sender{ID: "bob"},
		Chat:   message.Chat{ID: "chat-1", Type: message.ChatDM},
	}
	err := ch.SimulateMessage(msg)
	if !errors.Is(err, channel.ErrDenied) {
		t.Errorf("SimulateMessage = %v, want ErrDenied", err)
	}
}

func TestMockChannel_SimulateDeniedWithoutAllowList(t *testing.T) {
	t.Parallel()
	ch := NewMockChannel("test", nil)

	msg := message.InboundMessage{
		Sender: message.Sender{ID: "alice"},
		Chat:   message.Chat{ID: "chat-1", Type: message.ChatDM},
	}
	err := ch.SimulateMessage(msg)
	if !errors.Is(err, channel.ErrDenied) {
		t.Errorf("SimulateMessage without allow-list = %v, want ErrDenied", err)
	}
}

func TestMockChannel_SimulateWithoutInbox(t *testing.T) {
	t.Parallel()
	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := NewMockChannel("test", al)
	// No inbox set — should return ErrNoInbox.

	msg := message.InboundMessage{
		Sender: message.Sender{ID: "alice"},
		Chat:   message.Chat{ID: "chat-1", Type: message.ChatDM},
	}
	err := ch.SimulateMessage(msg)
	if !errors.Is(err, channel.ErrNoInbox) {
		t.Errorf("SimulateMessage without inbox = %v, want ErrNoInbox", err)
	}
}

func TestMockChannel_Reset(t *testing.T) {
	t.Parallel()
	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := NewMockChannel("test", al)

	_ = ch.Send(context.Background(), message.OutboundMessage{
		Chat:   message.Chat{ID: "chat-1"},
		Blocks: []message.ContentBlock{message.NewTextBlock("hello")},
	})

	if len(ch.SentMessages()) != 1 {
		t.Fatal("expected 1 sent message before reset")
	}

	ch.Reset()

	if len(ch.SentMessages()) != 0 {
		t.Error("expected 0 sent messages after reset")
	}
}

func TestMockChannel_ConcurrentSendAndRead(t *testing.T) {
	t.Parallel()
	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := NewMockChannel("test", al)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				msg := message.OutboundMessage{
					Channel: "test",
					Chat:    message.Chat{ID: "chat-1"},
					Blocks:  []message.ContentBlock{message.NewTextBlock("msg")},
				}
				_ = ch.Send(context.Background(), msg)
				_ = ch.SentMessages()
			}
		}()
	}
	wg.Wait()
}

func TestMockStreamingChannel_SendStream(t *testing.T) {
	t.Parallel()
	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := NewMockStreamingChannel("test", al)

	if !ch.SupportsStreaming() {
		t.Error("mock streaming channel should support streaming by default")
	}

	stream := make(chan string, 3)
	stream <- "hello "
	stream <- "world"
	stream <- "!"
	close(stream)

	if err := ch.SendStream(context.Background(), message.Chat{}, stream); err != nil {
		t.Fatalf("SendStream: %v", err)
	}

	chunks := ch.StreamChunks()
	if len(chunks) != 3 {
		t.Fatalf("expected 3 stream chunks, got %d", len(chunks))
	}
}

func TestMockStreamingChannel_SendTyping(t *testing.T) {
	t.Parallel()
	al := channel.NewAllowList([]string{"alice"}, nil)
	ch := NewMockStreamingChannel("test", al)

	chat := message.Chat{ID: "chat-1", Type: message.ChatDM}
	if err := ch.SendTyping(context.Background(), chat); err != nil {
		t.Fatalf("SendTyping: %v", err)
	}

	chats := ch.TypingChats()
	if len(chats) != 1 {
		t.Fatalf("expected 1 typing chat, got %d", len(chats))
	}
	if chats[0].ID != "chat-1" {
		t.Errorf("typing chat ID = %q, want %q", chats[0].ID, "chat-1")
	}
}
