package security

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactingHandler_RedactsMessage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewRedactor()
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner, r)
	logger := slog.New(handler)

	logger.Info("key is sk-abcdefghijklmnopqrstuvwxyz")

	output := buf.String()
	if strings.Contains(output, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("secret found in log output: %s", output)
	}
	if !strings.Contains(output, RedactPlaceholder) {
		t.Errorf("expected placeholder in output: %s", output)
	}
}

func TestRedactingHandler_RedactsAttributes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewRedactor()
	r.AddLiteral("super-secret-value")

	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner, r)
	logger := slog.New(handler)

	logger.Info("test", "token", "super-secret-value", "safe", "visible")

	output := buf.String()
	if strings.Contains(output, "super-secret-value") {
		t.Errorf("secret found in attributes: %s", output)
	}
	if !strings.Contains(output, "visible") {
		t.Errorf("safe value missing from output: %s", output)
	}
}

func TestRedactingHandler_WithAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewRedactor()
	r.AddLiteral("persistent-secret")

	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner, r)
	logger := slog.New(handler).With("api_key", "persistent-secret")

	logger.Info("test message")

	output := buf.String()
	if strings.Contains(output, "persistent-secret") {
		t.Errorf("secret found in WithAttrs output: %s", output)
	}
}

func TestRedactingHandler_WithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewRedactor()

	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner, r)
	logger := slog.New(handler).WithGroup("auth")

	logger.Info("attempt", "key", "sk-abcdefghijklmnopqrstuvwxyz")

	output := buf.String()
	if strings.Contains(output, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("secret found in group output: %s", output)
	}
}

func TestRedactingHandler_Enabled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewRedactor()

	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := NewRedactingHandler(inner, r)

	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug to be disabled with warn level")
	}
	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("expected error to be enabled with warn level")
	}
}

func TestRedactingHandler_NoSecrets(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewRedactor()
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner, r)
	logger := slog.New(handler)

	logger.Info("normal message", "key", "value")

	output := buf.String()
	if strings.Contains(output, RedactPlaceholder) {
		t.Errorf("unexpected redaction in output: %s", output)
	}
	if !strings.Contains(output, "normal message") {
		t.Errorf("message missing from output: %s", output)
	}
}

func TestRedactingHandler_WithGroup_NoSliceAliasing(t *testing.T) {
	t.Parallel()

	r := NewRedactor()
	var buf1, buf2 bytes.Buffer
	inner1 := slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelDebug})
	parent := NewRedactingHandler(inner1, r)

	// Create two children from the same parent.
	child1 := parent.WithGroup("groupA").(*RedactingHandler)
	child2 := parent.WithGroup("groupB").(*RedactingHandler)

	// Verify their groups slices are independent.
	if len(child1.groups) != 1 || child1.groups[0] != "groupA" {
		t.Errorf("child1.groups = %v, want [groupA]", child1.groups)
	}
	if len(child2.groups) != 1 || child2.groups[0] != "groupB" {
		t.Errorf("child2.groups = %v, want [groupB]", child2.groups)
	}

	// Verify further nesting doesn't affect siblings.
	inner2 := slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelDebug})
	parent2 := NewRedactingHandler(inner2, r)
	// Pre-populate groups slice to capacity 1, triggering aliasing if append is used.
	base := parent2.WithGroup("base").(*RedactingHandler)
	nested1 := base.WithGroup("nested1").(*RedactingHandler)
	nested2 := base.WithGroup("nested2").(*RedactingHandler)

	if nested1.groups[1] != "nested1" {
		t.Errorf("nested1.groups[1] = %q, want %q", nested1.groups[1], "nested1")
	}
	if nested2.groups[1] != "nested2" {
		t.Errorf("nested2.groups[1] = %q, want %q", nested2.groups[1], "nested2")
	}
}

func TestRedactingHandler_GroupAttr(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewRedactor()
	r.AddLiteral("nested-secret")

	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner, r)
	logger := slog.New(handler)

	logger.Info("test",
		slog.Group("request",
			slog.String("token", "nested-secret"),
			slog.String("path", "/api/v1"),
		),
	)

	output := buf.String()
	if strings.Contains(output, "nested-secret") {
		t.Errorf("secret found in group attribute: %s", output)
	}
}
