package channel

import (
	"context"
	"errors"
	"testing"

	"github.com/flemzord/sclaw/pkg/message"
)

func TestDispatcher_RegisterAndGet(t *testing.T) {
	t.Parallel()
	d := NewDispatcher()
	al := NewAllowList([]string{"alice"}, nil)
	ch := NewMockChannel("telegram", al)

	if err := d.Register("telegram", ch); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := d.Get("telegram")
	if !ok {
		t.Fatal("Get returned false for registered channel")
	}
	if got != ch {
		t.Error("Get returned wrong channel instance")
	}
}

func TestDispatcher_RegisterDuplicate(t *testing.T) {
	t.Parallel()
	d := NewDispatcher()
	al := NewAllowList([]string{"alice"}, nil)
	ch := NewMockChannel("telegram", al)

	if err := d.Register("telegram", ch); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := d.Register("telegram", ch)
	if !errors.Is(err, ErrDuplicateChannel) {
		t.Errorf("second Register = %v, want ErrDuplicateChannel", err)
	}
}

func TestDispatcher_GetMissing(t *testing.T) {
	t.Parallel()
	d := NewDispatcher()
	_, ok := d.Get("nonexistent")
	if ok {
		t.Error("Get should return false for unknown channel")
	}
}

func TestDispatcher_SendDispatchesToCorrectChannel(t *testing.T) {
	t.Parallel()
	d := NewDispatcher()
	al := NewAllowList([]string{"alice"}, nil)
	ch1 := NewMockChannel("ch1", al)
	ch2 := NewMockChannel("ch2", al)
	_ = d.Register("ch1", ch1)
	_ = d.Register("ch2", ch2)

	msg := message.OutboundMessage{
		Channel: "ch2",
		Chat:    message.Chat{ID: "chat-1"},
		Blocks:  []message.ContentBlock{message.NewTextBlock("hello")},
	}

	if err := d.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(ch1.SentMessages()) != 0 {
		t.Error("ch1 should not have received any messages")
	}
	if len(ch2.SentMessages()) != 1 {
		t.Fatalf("ch2 should have received 1 message, got %d", len(ch2.SentMessages()))
	}
	if ch2.SentMessages()[0].Blocks[0].Text != "hello" {
		t.Error("ch2 received wrong message content")
	}
}

func TestDispatcher_SendUnknownChannel(t *testing.T) {
	t.Parallel()
	d := NewDispatcher()
	msg := message.OutboundMessage{
		Channel: "unknown",
		Chat:    message.Chat{ID: "chat-1"},
	}
	err := d.Send(context.Background(), msg)
	if !errors.Is(err, ErrNoChannel) {
		t.Errorf("Send = %v, want ErrNoChannel", err)
	}
}

func TestDispatcher_Channels(t *testing.T) {
	t.Parallel()
	d := NewDispatcher()
	al := NewAllowList([]string{"alice"}, nil)
	_ = d.Register("a", NewMockChannel("a", al))
	_ = d.Register("b", NewMockChannel("b", al))

	names := d.Channels()
	if len(names) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(names))
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["a"] || !found["b"] {
		t.Errorf("unexpected channel names: %v", names)
	}
}

func TestDispatcher_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	d := NewDispatcher()
	al := NewAllowList([]string{"alice"}, nil)
	ch := NewMockChannel("test", al)
	_ = d.Register("test", ch)

	// Hammer the dispatcher concurrently to trigger race detection.
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				msg := message.OutboundMessage{
					Channel: "test",
					Chat:    message.Chat{ID: "chat-1"},
					Blocks:  []message.ContentBlock{message.NewTextBlock("msg")},
				}
				_ = d.Send(context.Background(), msg)
				d.Get("test")
				d.Channels()
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
