package agent

import (
	"encoding/json"

	"github.com/flemzord/sclaw/internal/provider"
)

// loopDetector tracks repeated identical tool calls to detect stuck loops.
type loopDetector struct {
	threshold int
	counts    map[string]int
}

func newLoopDetector(threshold int) *loopDetector {
	return &loopDetector{
		threshold: threshold,
		counts:    make(map[string]int),
	}
}

// normalizeArgs returns a canonical JSON representation of args so that
// semantically identical payloads with different key ordering produce the
// same string (e.g. {"a":1,"b":2} and {"b":2,"a":1}).
func normalizeArgs(args json.RawMessage) string {
	var m any
	if err := json.Unmarshal(args, &m); err != nil {
		// Fallback to raw bytes if the JSON is invalid.
		return string(args)
	}
	normalized, err := json.Marshal(m)
	if err != nil {
		return string(args)
	}
	return string(normalized)
}

// record registers a tool call and reports whether the loop threshold
// has been reached for this exact call signature.
func (d *loopDetector) record(name string, args json.RawMessage) bool {
	key := name + ":" + normalizeArgs(args)
	d.counts[key]++
	return d.counts[key] >= d.threshold
}

// tokenTracker accumulates token usage and checks against a budget.
//
// It is not concurrent-safe by design: each instance is owned by a single
// goroutine (the Run or RunStream loop).
type tokenTracker struct {
	budget int
	usage  provider.TokenUsage
}

func newTokenTracker(budget int) *tokenTracker {
	return &tokenTracker{budget: budget}
}

func (t *tokenTracker) add(usage provider.TokenUsage) {
	t.usage.PromptTokens += usage.PromptTokens
	t.usage.CompletionTokens += usage.CompletionTokens
	t.usage.TotalTokens += usage.TotalTokens
}

// exceeded reports whether the cumulative token usage has reached the budget.
// A zero budget means unlimited and never exceeds.
func (t *tokenTracker) exceeded() bool {
	return t.budget > 0 && t.usage.TotalTokens >= t.budget
}

func (t *tokenTracker) total() provider.TokenUsage {
	return t.usage
}
