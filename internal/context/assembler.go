package ctxengine

import (
	"context"
	"strings"

	"github.com/flemzord/sclaw/internal/provider"
)

// AssemblyRequest contains the inputs for context assembly.
type AssemblyRequest struct {
	// WindowSize is the provider's context window in tokens.
	WindowSize int

	// SystemParts are the components of the system prompt (SOUL.md, skills, etc.).
	SystemParts []string

	// Tools are the tool definitions available to the model.
	Tools []provider.ToolDefinition

	// History is the full conversation history for the session.
	History []provider.LLMMessage

	// MemoryFacts are pre-retrieved relevant facts to inject.
	MemoryFacts []string
}

// AssemblyResult is the output of context assembly.
type AssemblyResult struct {
	// SystemPrompt is the fully assembled system prompt.
	SystemPrompt string

	// Messages is the (possibly compacted) conversation history.
	Messages []provider.LLMMessage

	// Tools is the tool definitions (passed through).
	Tools []provider.ToolDefinition

	// Budget is the final token budget breakdown.
	Budget ContextBudget

	// Compacted is true if compaction was triggered during assembly.
	Compacted bool
}

// ContextAssembler builds the complete context for an agent request,
// managing token budgets and triggering compaction when needed.
type ContextAssembler struct {
	estimator TokenEstimator
	compactor *Compactor
	config    ContextConfig
}

// NewContextAssembler creates a ContextAssembler with the given estimator and config.
func NewContextAssembler(estimator TokenEstimator, cfg ContextConfig) *ContextAssembler {
	cfg = cfg.withDefaults()
	return &ContextAssembler{
		estimator: estimator,
		config:    cfg,
	}
}

// SetCompactor attaches a compactor for automatic history compaction.
func (a *ContextAssembler) SetCompactor(c *Compactor) {
	a.compactor = c
}

// Assemble builds the agent context from the given inputs.
//
// The assembly process:
//  1. Compute fixed costs (system prompt, tools, reserved)
//  2. Trigger proactive compaction if history exceeds threshold
//  3. Trim history to fit remaining budget
//  4. Return the assembled result with budget breakdown
func (a *ContextAssembler) Assemble(ctx context.Context, req AssemblyRequest) (AssemblyResult, error) {
	windowSize := req.WindowSize
	if a.config.MaxContextTokens > 0 {
		windowSize = a.config.MaxContextTokens
	}

	// Build system prompt from parts.
	systemPrompt := strings.Join(req.SystemParts, "\n\n")
	// Append memory facts to the system prompt, respecting MaxMemoryFacts.
	facts := req.MemoryFacts
	if a.config.MaxMemoryFacts > 0 && len(facts) > a.config.MaxMemoryFacts {
		facts = facts[:a.config.MaxMemoryFacts]
	}
	if len(facts) > 0 {
		memorySection := formatMemoryFacts(facts)
		systemPrompt = systemPrompt + "\n\n" + memorySection
	}

	// Compute fixed costs.
	systemTokens := a.estimator.Estimate(systemPrompt)
	toolTokens := EstimateToolDefinitions(a.estimator, req.Tools)

	budget := ContextBudget{
		WindowSize: windowSize,
		System:     systemTokens,
		Tools:      toolTokens,
		Reserved:   a.config.ReservedForReply,
	}

	// Compute available tokens for history.
	historyBudget := windowSize - systemTokens - toolTokens - a.config.ReservedForReply
	if historyBudget < 0 {
		historyBudget = 0
	}

	history := req.History
	compacted := false

	// Proactive compaction: compact if history exceeds threshold.
	if a.compactor != nil && a.compactor.ShouldCompact(history) {
		var err error
		history, err = a.compactor.Compact(ctx, history)
		if err != nil {
			return AssemblyResult{}, err
		}
		compacted = true
	}

	// Trim history to fit budget.
	history = a.trimHistory(history, historyBudget)

	budget.History = EstimateMessages(a.estimator, history)

	return AssemblyResult{
		SystemPrompt: systemPrompt,
		Messages:     history,
		Tools:        req.Tools,
		Budget:       budget,
		Compacted:    compacted,
	}, nil
}

// trimHistory removes the oldest messages (preserving the most recent)
// until the history fits within the token budget.
// A summary message (system role at position 0) is preserved if present.
func (a *ContextAssembler) trimHistory(history []provider.LLMMessage, budget int) []provider.LLMMessage {
	if len(history) == 0 {
		return history
	}

	tokens := EstimateMessages(a.estimator, history)
	if tokens <= budget {
		return history
	}

	// Check if the first message is a summary (from a previous compaction).
	hasSummary := history[0].Role == provider.MessageRoleSystem

	// Remove oldest messages (after the summary if present) until we fit.
	start := 0
	if hasSummary {
		start = 1
	}

	for tokens > budget && start < len(history)-1 {
		start++
		remaining := history[start:]
		tokens = EstimateMessages(a.estimator, remaining)
		if hasSummary {
			tokens += EstimateMessages(a.estimator, history[:1])
		}
	}

	if hasSummary && start > 1 {
		result := make([]provider.LLMMessage, 0, 1+len(history)-start)
		result = append(result, history[0])
		result = append(result, history[start:]...)
		return result
	}

	return history[start:]
}

// formatMemoryFacts formats memory facts for inclusion in the system prompt.
func formatMemoryFacts(facts []string) string {
	if len(facts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Relevant Memory\n\n")
	for _, fact := range facts {
		b.WriteString("- ")
		b.WriteString(fact)
		b.WriteString("\n")
	}
	return b.String()
}
