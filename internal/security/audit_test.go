package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuditLogger_WritesJSONL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	logger := NewAuditLogger(AuditLoggerConfig{
		Writer: &buf,
		Now:    func() time.Time { return fixedTime },
	})

	logger.Log(AuditEvent{
		Type:      EventMessage,
		SessionID: "sess-1",
		Channel:   "slack",
		SenderID:  "user-1",
		Detail:    "hello world",
	})

	var got AuditEvent
	if err := json.NewDecoder(&buf).Decode(&got); err != nil {
		t.Fatalf("failed to decode JSONL: %v", err)
	}

	if got.Type != EventMessage {
		t.Errorf("type = %q, want %q", got.Type, EventMessage)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("session_id = %q, want %q", got.SessionID, "sess-1")
	}
	if got.Timestamp != fixedTime {
		t.Errorf("timestamp = %v, want %v", got.Timestamp, fixedTime)
	}
}

func TestAuditLogger_RedactsDetail(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewRedactor()
	r.AddLiteral("my-secret-key")

	logger := NewAuditLogger(AuditLoggerConfig{
		Writer:   &buf,
		Redactor: r,
	})

	logger.Log(AuditEvent{
		Type:   EventToolCall,
		Detail: "calling with my-secret-key",
		Metadata: map[string]string{
			"arg": "value is my-secret-key here",
		},
	})

	output := buf.String()
	if strings.Contains(output, "my-secret-key") {
		t.Errorf("secret found in audit output: %s", output)
	}
	if !strings.Contains(output, RedactPlaceholder) {
		t.Errorf("expected placeholder in audit output: %s", output)
	}
}

func TestAuditLogger_OnEventCallback(t *testing.T) {
	t.Parallel()

	var events []AuditEvent
	logger := NewAuditLogger(AuditLoggerConfig{
		OnEvent: func(e AuditEvent) {
			events = append(events, e)
		},
	})

	logger.Log(AuditEvent{Type: EventAuthSuccess, SenderID: "admin"})
	logger.Log(AuditEvent{Type: EventAuthFailure, SenderID: "hacker"})

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Type != EventAuthSuccess {
		t.Errorf("events[0].type = %q, want %q", events[0].Type, EventAuthSuccess)
	}
	if events[1].Type != EventAuthFailure {
		t.Errorf("events[1].type = %q, want %q", events[1].Type, EventAuthFailure)
	}
}

func TestAuditLogger_AllEventTypes(t *testing.T) {
	t.Parallel()

	types := []EventType{
		EventMessage, EventToolCall, EventToolResult, EventApproval,
		EventAuthSuccess, EventAuthFailure, EventConfigChange,
		EventSessionCreate, EventSessionDelete, EventRateLimit,
	}

	var buf bytes.Buffer
	logger := NewAuditLogger(AuditLoggerConfig{Writer: &buf})

	for _, et := range types {
		logger.Log(AuditEvent{Type: et})
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != len(types) {
		t.Fatalf("got %d lines, want %d", len(lines), len(types))
	}
}

func TestAuditLogger_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewAuditLogger(AuditLoggerConfig{Writer: &buf})

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Log(AuditEvent{Type: EventMessage, Detail: "concurrent"})
		}()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 50 {
		t.Fatalf("got %d lines, want 50", len(lines))
	}
}

func TestAuditLogger_NilWriter(t *testing.T) {
	t.Parallel()

	var called bool
	logger := NewAuditLogger(AuditLoggerConfig{
		OnEvent: func(_ AuditEvent) { called = true },
	})

	// Should not panic with nil writer.
	logger.Log(AuditEvent{Type: EventMessage})

	if !called {
		t.Error("expected OnEvent to be called even with nil writer")
	}
}

// errWriter always returns an error on Write to simulate a failing io.Writer.
type errWriter struct{}

func (errWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestAuditLogger_WriteErrors_CountsFailures(t *testing.T) {
	t.Parallel()

	logger := NewAuditLogger(AuditLoggerConfig{
		Writer: errWriter{},
	})

	logger.Log(AuditEvent{Type: EventMessage})
	logger.Log(AuditEvent{Type: EventMessage})

	if got := logger.WriteErrors(); got != 2 {
		t.Errorf("WriteErrors() = %d, want 2", got)
	}
}

func TestAuditLogger_WriteErrors_ZeroOnSuccess(t *testing.T) {
	t.Parallel()

	logger := NewAuditLogger(AuditLoggerConfig{
		Writer: io.Discard,
	})

	logger.Log(AuditEvent{Type: EventMessage})
	logger.Log(AuditEvent{Type: EventMessage})

	if got := logger.WriteErrors(); got != 0 {
		t.Errorf("WriteErrors() = %d, want 0", got)
	}
}
