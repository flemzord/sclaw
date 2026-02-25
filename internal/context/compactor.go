package ctxengine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/flemzord/sclaw/internal/provider"
)

// ErrCompactionFailed indicates that compaction could not produce a summary.
var ErrCompactionFailed = errors.New("ctxengine: compaction failed")

// Summarizer produces a condensed summary of a conversation segment.
// The concrete implementation will typically call the internal provider.
type Summarizer interface {
	Summarize(ctx context.Context, messages []provider.LLMMessage) (string, error)
}

// Compactor manages conversation history compaction.
type Compactor struct {
	summarizer Summarizer
	estimator  TokenEstimator
	config     ContextConfig
}

// NewCompactor creates a Compactor. A nil summarizer disables summary
// generation — compaction still works by dropping old messages.
func NewCompactor(summarizer Summarizer, estimator TokenEstimator, cfg ContextConfig) *Compactor {
	cfg = cfg.withDefaults()
	return &Compactor{
		summarizer: summarizer,
		estimator:  estimator,
		config:     cfg,
	}
}

// ShouldCompact reports whether the history exceeds the compaction threshold.
func (c *Compactor) ShouldCompact(history []provider.LLMMessage) bool {
	return len(history) > c.config.CompactionThreshold
}

// Compact summarizes old messages and keeps the RetainRecent most recent.
//
// When a Summarizer is configured, the old messages are summarized into a
// single system message prepended to the retained tail. Without a Summarizer,
// old messages are silently dropped (drop-compaction).
//
// If the history is shorter than or equal to RetainRecent, it is returned unchanged.
func (c *Compactor) Compact(ctx context.Context, history []provider.LLMMessage) ([]provider.LLMMessage, error) {
	retain := c.config.RetainRecent
	if len(history) <= retain {
		return history, nil
	}

	old := history[:len(history)-retain]
	recent := history[len(history)-retain:]

	summary, err := c.summarize(ctx, old)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCompactionFailed, err)
	}

	return c.prependSummary(summary, recent), nil
}

// EmergencyCompact is a last-resort compaction triggered by ErrContextLength.
// It keeps only the EmergencyRetain most recent messages without attempting
// a summary (the provider may be unable to process any request at this point).
// The returned slice is a copy so the caller's original is not mutated.
func (c *Compactor) EmergencyCompact(_ context.Context, history []provider.LLMMessage) ([]provider.LLMMessage, error) {
	retain := c.config.EmergencyRetain
	if len(history) <= retain {
		return history, nil
	}
	result := make([]provider.LLMMessage, retain)
	copy(result, history[len(history)-retain:])
	return result, nil
}

// summarize calls the Summarizer if available, otherwise returns an empty string.
func (c *Compactor) summarize(ctx context.Context, messages []provider.LLMMessage) (string, error) {
	if c.summarizer == nil || len(messages) == 0 {
		return "", nil
	}
	return c.summarizer.Summarize(ctx, messages)
}

// prependSummary builds the compacted history. If summary is non-empty, it is
// inserted as a system message before the recent messages.
func (c *Compactor) prependSummary(summary string, recent []provider.LLMMessage) []provider.LLMMessage {
	if summary == "" {
		result := make([]provider.LLMMessage, len(recent))
		copy(result, recent)
		return result
	}

	result := make([]provider.LLMMessage, 0, 1+len(recent))
	result = append(result, provider.LLMMessage{
		Role:    provider.MessageRoleSystem,
		Content: formatSummary(summary),
	})
	return append(result, recent...)
}

// formatSummary wraps the raw summary text in a labelled block.
func formatSummary(summary string) string {
	var b strings.Builder
	b.WriteString("[Conversation Summary]\n")
	b.WriteString(summary)
	return b.String()
}
