package ctxengine

import (
	"encoding/json"
	"strings"

	"github.com/flemzord/sclaw/internal/provider"
)

// TokenEstimator estimates the token count of a string.
type TokenEstimator interface {
	Estimate(text string) int
}

// CharEstimator estimates tokens using a simple characters-per-token ratio.
// A ratio of ~4 works well for English; ~3 for French or other Latin languages.
type CharEstimator struct {
	CharsPerToken float64
}

// NewCharEstimator creates a CharEstimator with the given ratio.
// If charsPerToken is <= 0, defaults to 4.0 (English approximation).
func NewCharEstimator(charsPerToken float64) *CharEstimator {
	if charsPerToken <= 0 {
		charsPerToken = 4.0
	}
	return &CharEstimator{CharsPerToken: charsPerToken}
}

// Estimate returns the estimated token count for the given text.
func (e *CharEstimator) Estimate(text string) int {
	if len(text) == 0 {
		return 0
	}
	tokens := float64(len(text)) / e.CharsPerToken
	// Always round up to avoid underestimation.
	return int(tokens) + 1
}

// ContextBudget tracks token allocation across prompt sections.
type ContextBudget struct {
	WindowSize int // total context window in tokens
	System     int // tokens used by system prompt
	Tools      int // tokens used by tool schemas
	Memory     int // tokens used by injected memory facts
	History    int // tokens used by conversation history
	Reserved   int // reserved for model reply
}

// Used returns the total number of tokens consumed across all sections.
func (b ContextBudget) Used() int {
	return b.System + b.Tools + b.Memory + b.History + b.Reserved
}

// Available returns the number of tokens remaining for additional content.
// Returns 0 if the budget is already exceeded.
func (b ContextBudget) Available() int {
	avail := b.WindowSize - b.Used()
	if avail < 0 {
		return 0
	}
	return avail
}

// Exceeded reports whether total usage exceeds the context window.
func (b ContextBudget) Exceeded() bool {
	return b.Used() > b.WindowSize
}

// EstimateMessages returns the total estimated tokens for a slice of LLM messages.
func EstimateMessages(estimator TokenEstimator, messages []provider.LLMMessage) int {
	total := 0
	for i := range messages {
		// Per-message overhead: role tokens + formatting (~4 tokens).
		total += 4

		if len(messages[i].ContentParts) > 0 {
			for _, p := range messages[i].ContentParts {
				switch p.Type {
				case provider.ContentPartText:
					total += estimator.Estimate(p.Text)
				case provider.ContentPartImageURL:
					// Conservative estimate for "auto" detail images (~765 tokens).
					total += 765
				}
			}
		} else {
			total += estimator.Estimate(messages[i].Content)
		}

		if messages[i].Name != "" {
			total += estimator.Estimate(messages[i].Name)
		}

		for j := range messages[i].ToolCalls {
			total += estimator.Estimate(messages[i].ToolCalls[j].Name)
			total += estimator.Estimate(string(messages[i].ToolCalls[j].Arguments))
		}
	}
	return total
}

// EstimateTools returns the total estimated tokens for tool definitions
// using per-field estimation.
func EstimateTools(estimator TokenEstimator, tools []provider.ToolDefinition) int {
	total := 0
	for i := range tools {
		total += estimator.Estimate(tools[i].Name)
		total += estimator.Estimate(tools[i].Description)
		if tools[i].Parameters != nil {
			total += estimator.Estimate(string(tools[i].Parameters))
		}
	}
	return total
}

// EstimateToolDefinitions returns the estimated token count for tool definitions
// serialized as JSON (how they appear in the actual prompt).
func EstimateToolDefinitions(estimator TokenEstimator, tools []provider.ToolDefinition) int {
	if len(tools) == 0 {
		return 0
	}
	data, err := json.Marshal(tools)
	if err != nil {
		return EstimateTools(estimator, tools)
	}
	return estimator.Estimate(string(data))
}

// EstimateSystemPrompt returns the estimated tokens for a system prompt
// assembled from multiple parts joined by double newlines.
func EstimateSystemPrompt(estimator TokenEstimator, parts []string) int {
	joined := strings.Join(parts, "\n\n")
	return estimator.Estimate(joined)
}
