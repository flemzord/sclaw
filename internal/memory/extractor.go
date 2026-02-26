package memory

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
)

var factIDCounter atomic.Uint64

// FactExtractor analyzes exchanges to extract facts for long-term memory.
// It uses an LLM provider (via the Summarizer-like interface) to identify
// facts worth remembering.
type FactExtractor interface {
	Extract(ctx context.Context, exchange Exchange) ([]Fact, error)
}

// LLMExtractor uses an LLM to analyze exchanges and extract facts.
type LLMExtractor struct {
	provider provider.Provider
}

// NewLLMExtractor creates an extractor that uses the given provider
// (typically the internal provider from the chain) to analyze exchanges.
func NewLLMExtractor(p provider.Provider) *LLMExtractor {
	return &LLMExtractor{provider: p}
}

// Compile-time interface check.
var _ FactExtractor = (*LLMExtractor)(nil)

const extractionPrompt = `Analyze the following exchange and extract important facts about the user.
Return one fact per line. If there are no facts worth remembering, return "NONE".
Only extract factual information (preferences, personal details, decisions, goals).

User: %s
Assistant: %s

Facts:`

// Extract analyzes an exchange and returns facts to index.
// Returns nil (not an error) if nothing worth remembering is found.
func (e *LLMExtractor) Extract(ctx context.Context, exchange Exchange) ([]Fact, error) {
	prompt := fmt.Sprintf(
		extractionPrompt,
		exchange.UserMessage.Content,
		exchange.AssistantMessage.Content,
	)

	resp, err := e.provider.Complete(ctx, provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("memory: extraction failed: %w", err)
	}

	return parseExtractedFacts(resp.Content, exchange), nil
}

// parseExtractedFacts parses the LLM response into Fact structs.
func parseExtractedFacts(response string, exchange Exchange) []Fact {
	if response == "" || response == "NONE" {
		return nil
	}

	lines := splitLines(response)
	facts := make([]Fact, 0, len(lines))
	now := time.Now()

	for i, line := range lines {
		line = trimBullet(line)
		if line == "" || line == "NONE" {
			continue
		}

		facts = append(facts, Fact{
			ID:        nextFactID(now, i),
			Content:   line,
			Source:    exchange.SessionID,
			CreatedAt: now,
		})
	}

	return facts
}

func nextFactID(now time.Time, index int) string {
	seq := factIDCounter.Add(1)
	return fmt.Sprintf("%d-%d-%d", now.UnixNano(), index, seq)
}

// splitLines splits text by newlines, trimming whitespace and filtering blanks.
func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// trimBullet removes leading bullet markers ("- ", "* ", "1. ", etc.).
func trimBullet(s string) string {
	if len(s) == 0 {
		return s
	}
	// "- " or "* "
	if len(s) >= 2 && (s[0] == '-' || s[0] == '*') && s[1] == ' ' {
		return s[2:]
	}
	// "1. " style numbered lists
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && s[i] == '.' && i+1 < len(s) && s[i+1] == ' ' {
		return s[i+2:]
	}
	return s
}

// NopExtractor is a no-op extractor for when memory extraction is disabled.
type NopExtractor struct{}

// Compile-time interface check.
var _ FactExtractor = (*NopExtractor)(nil)

// Extract always returns nil (no facts), implementing graceful degradation.
func (NopExtractor) Extract(_ context.Context, _ Exchange) ([]Fact, error) {
	return nil, nil
}
