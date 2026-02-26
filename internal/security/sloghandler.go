package security

import (
	"context"
	"log/slog"
)

// RedactingHandler wraps a slog.Handler and redacts secrets from all
// string-valued attributes before passing them to the inner handler.
// This ensures no secret leaks into log output regardless of where
// the log call originates.
type RedactingHandler struct {
	inner    slog.Handler
	redactor *Redactor
	groups   []string
}

// Compile-time check.
var _ slog.Handler = (*RedactingHandler)(nil)

// NewRedactingHandler creates a handler that wraps inner, applying
// redactor to every string attribute value.
func NewRedactingHandler(inner slog.Handler, redactor *Redactor) *RedactingHandler {
	return &RedactingHandler{
		inner:    inner,
		redactor: redactor,
	}
}

// Enabled delegates to the inner handler.
func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle redacts string values in the record's attributes and message,
// then delegates to the inner handler.
func (h *RedactingHandler) Handle(ctx context.Context, record slog.Record) error {
	// Redact the message itself.
	record.Message = h.redactor.Redact(record.Message)

	// Build a new record with redacted attributes.
	redacted := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)

	// Redact inline attributes from this specific log call.
	record.Attrs(func(a slog.Attr) bool {
		redacted.AddAttrs(h.redactAttr(a))
		return true
	})

	return h.inner.Handle(ctx, redacted)
}

// WithAttrs returns a new handler with pre-resolved, redacted attributes.
func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.redactAttr(a)
	}
	return &RedactingHandler{
		inner:    h.inner.WithAttrs(redacted),
		redactor: h.redactor,
		groups:   h.groups,
	}
}

// WithGroup returns a new handler with the given group name.
// A fresh slice is allocated to prevent slice aliasing with the parent handler.
func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	newGroups := make([]string, len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups[len(h.groups)] = name
	return &RedactingHandler{
		inner:    h.inner.WithGroup(name),
		redactor: h.redactor,
		groups:   newGroups,
	}
}

// redactAttr recursively redacts string values in an attribute.
func (h *RedactingHandler) redactAttr(a slog.Attr) slog.Attr {
	// Resolve the attribute first so LogValuer, error, and fmt.Stringer
	// types are converted to their final representation.
	a.Value = a.Value.Resolve()

	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(h.redactor.Redact(a.Value.String()))
	case slog.KindGroup:
		attrs := a.Value.Group()
		redacted := make([]slog.Attr, len(attrs))
		for i, ga := range attrs {
			redacted[i] = h.redactAttr(ga)
		}
		a.Value = slog.GroupValue(redacted...)
	case slog.KindAny:
		// After Resolve(), remaining KindAny values (e.g., error types)
		// should have their string representation redacted.
		resolved := a.Value.String()
		redacted := h.redactor.Redact(resolved)
		if redacted != resolved {
			a.Value = slog.StringValue(redacted)
		}
	}
	return a
}
