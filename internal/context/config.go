// Package ctxengine implements LLM context management: prompt assembly,
// token budgeting, and conversation compaction.
package ctxengine

// ContextConfig holds the tuning knobs for the context engine.
type ContextConfig struct {
	// MaxContextTokens overrides the provider's ContextWindowSize().
	// 0 means use the provider's reported value.
	MaxContextTokens int

	// ReservedForReply is the number of tokens reserved for the model's response.
	ReservedForReply int

	// CompactionThreshold triggers proactive compaction when the history
	// exceeds this many messages.
	CompactionThreshold int

	// RetainRecent is the number of most-recent messages to keep after compaction.
	RetainRecent int

	// EmergencyRetain is the number of messages kept after an emergency
	// compaction (triggered by ErrContextLength).
	EmergencyRetain int

	// MaxMemoryTokens caps the token budget allocated to injected memory facts.
	MaxMemoryTokens int

	// MaxMemoryFacts caps the number of facts injected from long-term memory.
	MaxMemoryFacts int
}

// withDefaults returns a copy of cfg with zero-valued fields replaced by
// sensible defaults.
func (cfg ContextConfig) withDefaults() ContextConfig {
	if cfg.ReservedForReply == 0 {
		cfg.ReservedForReply = 1024
	}
	if cfg.CompactionThreshold == 0 {
		cfg.CompactionThreshold = 20
	}
	if cfg.RetainRecent == 0 {
		cfg.RetainRecent = 20
	}
	if cfg.EmergencyRetain == 0 {
		cfg.EmergencyRetain = 5
	}
	if cfg.MaxMemoryTokens == 0 {
		cfg.MaxMemoryTokens = 2000
	}
	if cfg.MaxMemoryFacts == 0 {
		cfg.MaxMemoryFacts = 10
	}
	return cfg
}
