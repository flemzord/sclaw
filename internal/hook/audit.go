package hook

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"sync"
	"time"
)

// AuditRecord is one JSON Lines entry written by AuditHook.
type AuditRecord struct {
	Timestamp    time.Time `json:"timestamp"`
	SessionID    string    `json:"session_id"`
	Channel      string    `json:"channel"`
	ChatID       string    `json:"chat_id"`
	SenderID     string    `json:"sender_id"`
	InboundText  string    `json:"inbound_text"`
	OutboundText string    `json:"outbound_text"`
	AgentID      string    `json:"agent_id"`
	Iterations   int       `json:"iterations"`
	StopReason   string    `json:"stop_reason"`
}

// AuditHook writes a JSON Lines audit log entry for every completed message.
// It runs at AfterSend with the lowest priority (runs last).
type AuditHook struct {
	writer io.Writer
	mu     sync.Mutex
	now    func() time.Time
}

// NewAuditHook creates an audit hook that writes JSON Lines to w.
// In production, w is typically an *os.File; in tests, a *bytes.Buffer.
func NewAuditHook(w io.Writer) *AuditHook {
	return &AuditHook{
		writer: w,
		now:    time.Now,
	}
}

// Compile-time interface check.
var _ Hook = (*AuditHook)(nil)

// Position returns AfterSend — the audit hook records completed exchanges.
func (a *AuditHook) Position() Position { return AfterSend }

// Priority returns math.MaxInt — the audit hook runs last among AfterSend hooks.
func (a *AuditHook) Priority() int { return math.MaxInt }

// Execute writes one JSON Lines record capturing the exchange details.
func (a *AuditHook) Execute(_ context.Context, hctx *Context) (Action, error) {
	record := AuditRecord{
		Timestamp:   a.now(),
		SenderID:    hctx.Inbound.Sender.ID,
		InboundText: hctx.Inbound.TextContent(),
	}

	if hctx.Session != nil {
		record.SessionID = hctx.Session.SessionID()
		record.AgentID = hctx.Session.AgentID()
		ch, chatID, _ := hctx.Session.SessionKey()
		record.Channel = ch
		record.ChatID = chatID
	}

	if hctx.Outbound != nil {
		record.OutboundText = hctx.Outbound.TextContent()
	}

	if hctx.Response != nil {
		record.Iterations = hctx.Response.Iterations
		record.StopReason = string(hctx.Response.StopReason)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := json.NewEncoder(a.writer).Encode(record); err != nil {
		return ActionContinue, err
	}
	return ActionContinue, nil
}
