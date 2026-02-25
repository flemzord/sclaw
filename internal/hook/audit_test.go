package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/pkg/message"
)

// mockSessionView implements SessionView for audit tests.
type mockSessionView struct {
	id       string
	channel  string
	chatID   string
	threadID string
	agentID  string
}

func (m *mockSessionView) SessionID() string { return m.id }

func (m *mockSessionView) SessionKey() (string, string, string) {
	return m.channel, m.chatID, m.threadID
}

func (m *mockSessionView) AgentID() string                  { return m.agentID }
func (m *mockSessionView) CreatedAt() time.Time             { return time.Time{} }
func (m *mockSessionView) GetMetadata(_ string) (any, bool) { return nil, false }

func TestAuditHook_Position(t *testing.T) {
	t.Parallel()
	h := NewAuditHook(&bytes.Buffer{})
	if h.Position() != AfterSend {
		t.Errorf("position = %q, want %q", h.Position(), AfterSend)
	}
}

func TestAuditHook_Priority(t *testing.T) {
	t.Parallel()
	h := NewAuditHook(&bytes.Buffer{})
	if h.Priority() != math.MaxInt {
		t.Errorf("priority = %d, want math.MaxInt", h.Priority())
	}
}

func TestAuditHook_WritesJSONLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewAuditHook(&buf)
	h.now = func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }

	outbound := message.NewTextMessage(message.Chat{ID: "C123"}, "Hello back!")
	resp := agent.Response{
		Content:    "Hello back!",
		Iterations: 1,
		StopReason: agent.StopReasonComplete,
	}

	hctx := &Context{
		Position: AfterSend,
		Inbound: message.InboundMessage{
			Sender: message.Sender{ID: "user-1"},
			Blocks: []message.ContentBlock{message.NewTextBlock("Hello")},
		},
		Session: &mockSessionView{
			id:      "sess-1",
			channel: "slack",
			chatID:  "C123",
			agentID: "agent-1",
		},
		Outbound: &outbound,
		Response: &resp,
		Metadata: make(map[string]any),
		Logger:   slog.Default(),
	}

	action, err := h.Execute(context.Background(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != ActionContinue {
		t.Errorf("action = %d, want ActionContinue", action)
	}

	// Parse the JSON line.
	var record AuditRecord
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}

	if record.SessionID != "sess-1" {
		t.Errorf("session_id = %q, want %q", record.SessionID, "sess-1")
	}
	if record.Channel != "slack" {
		t.Errorf("channel = %q, want %q", record.Channel, "slack")
	}
	if record.ChatID != "C123" {
		t.Errorf("chat_id = %q, want %q", record.ChatID, "C123")
	}
	if record.SenderID != "user-1" {
		t.Errorf("sender_id = %q, want %q", record.SenderID, "user-1")
	}
	if record.InboundText != "Hello" {
		t.Errorf("inbound_text = %q, want %q", record.InboundText, "Hello")
	}
	if record.OutboundText != "Hello back!" {
		t.Errorf("outbound_text = %q, want %q", record.OutboundText, "Hello back!")
	}
	if record.AgentID != "agent-1" {
		t.Errorf("agent_id = %q, want %q", record.AgentID, "agent-1")
	}
	if record.Iterations != 1 {
		t.Errorf("iterations = %d, want 1", record.Iterations)
	}
	if record.StopReason != string(agent.StopReasonComplete) {
		t.Errorf("stop_reason = %q, want %q", record.StopReason, agent.StopReasonComplete)
	}
}

func TestAuditHook_NilSession(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewAuditHook(&buf)

	hctx := &Context{
		Position: AfterSend,
		Inbound: message.InboundMessage{
			Sender: message.Sender{ID: "user-1"},
			Blocks: []message.ContentBlock{message.NewTextBlock("test")},
		},
		Metadata: make(map[string]any),
		Logger:   slog.Default(),
	}

	_, err := h.Execute(context.Background(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var record AuditRecord
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if record.SessionID != "" {
		t.Errorf("session_id = %q, want empty", record.SessionID)
	}
}

func TestAuditHook_IntegrationWithPipeline(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	audit := NewAuditHook(&buf)

	p := NewPipeline()
	p.Register(audit)

	outbound := message.NewTextMessage(message.Chat{ID: "C1"}, "Reply")
	resp := agent.Response{Content: "Reply", Iterations: 2, StopReason: agent.StopReasonComplete}

	hctx := &Context{
		Position: AfterSend,
		Inbound: message.InboundMessage{
			Sender: message.Sender{ID: "u1"},
			Blocks: []message.ContentBlock{message.NewTextBlock("Hey")},
		},
		Session: &mockSessionView{
			id:      "s1",
			channel: "telegram",
			chatID:  "chat-99",
			agentID: "bot-1",
		},
		Outbound: &outbound,
		Response: &resp,
		Metadata: make(map[string]any),
		Logger:   slog.Default(),
	}

	p.RunAfterSend(context.Background(), hctx)

	if buf.Len() == 0 {
		t.Fatal("expected audit output, got empty buffer")
	}

	var record AuditRecord
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if record.Channel != "telegram" {
		t.Errorf("channel = %q, want %q", record.Channel, "telegram")
	}
}
