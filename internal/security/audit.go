package security

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// EventType categorizes audit events.
type EventType string

// Audit event types covering all security-relevant interactions.
const (
	EventMessage       EventType = "message"
	EventToolCall      EventType = "tool_call"
	EventToolResult    EventType = "tool_result"
	EventApproval      EventType = "approval"
	EventAuthSuccess   EventType = "auth_success"
	EventAuthFailure   EventType = "auth_failure"
	EventConfigChange  EventType = "config_change"
	EventSessionCreate EventType = "session_create"
	EventSessionDelete EventType = "session_delete"
	EventRateLimit     EventType = "rate_limit"
)

// AuditEvent is a single audit log entry.
type AuditEvent struct {
	Timestamp time.Time         `json:"timestamp"`
	Type      EventType         `json:"type"`
	SessionID string            `json:"session_id,omitempty"`
	Channel   string            `json:"channel,omitempty"`
	ChatID    string            `json:"chat_id,omitempty"`
	SenderID  string            `json:"sender_id,omitempty"`
	AgentID   string            `json:"agent_id,omitempty"`
	ToolName  string            `json:"tool_name,omitempty"`
	Detail    string            `json:"detail,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// AuditLoggerConfig configures the audit logger.
type AuditLoggerConfig struct {
	// Writer is the destination for JSONL output. If nil, events are only
	// dispatched to OnEvent (useful for testing).
	Writer io.Writer

	// Redactor, if non-nil, is applied to Detail and Metadata values before writing.
	Redactor *Redactor

	// OnEvent, if non-nil, is called for every event (used in tests).
	OnEvent func(AuditEvent)

	// Now overrides time.Now for testing. Defaults to time.Now.
	Now func() time.Time
}

// AuditLogger writes structured audit events as JSONL with optional redaction.
type AuditLogger struct {
	writer   io.Writer
	redactor *Redactor
	onEvent  func(AuditEvent)
	now      func() time.Time
	mu       sync.Mutex
}

// NewAuditLogger creates an audit logger with the given configuration.
func NewAuditLogger(cfg AuditLoggerConfig) *AuditLogger {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &AuditLogger{
		writer:   cfg.Writer,
		redactor: cfg.Redactor,
		onEvent:  cfg.OnEvent,
		now:      now,
	}
}

// Log writes an audit event. The timestamp is set automatically.
// If a Redactor is configured, Detail and Metadata values are redacted.
// The caller's Metadata map is never mutated — a copy is made if redaction
// or serialization is needed.
func (l *AuditLogger) Log(event AuditEvent) {
	event.Timestamp = l.now()

	// Copy metadata to avoid mutating the caller's map.
	if len(event.Metadata) > 0 {
		cp := make(map[string]string, len(event.Metadata))
		for k, v := range event.Metadata {
			cp[k] = v
		}
		event.Metadata = cp
	}

	// Redact sensitive fields.
	if l.redactor != nil {
		event.Detail = l.redactor.Redact(event.Detail)
		for k, v := range event.Metadata {
			event.Metadata[k] = l.redactor.Redact(v)
		}
	}

	// Dispatch to test callback and write JSONL under the same lock
	// to ensure ordering consistency and protect the onEvent callback.
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.onEvent != nil {
		l.onEvent(event)
	}

	if l.writer != nil {
		_ = json.NewEncoder(l.writer).Encode(event)
	}
}
