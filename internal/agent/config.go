package agent

import "time"

// Default values for LoopConfig.
const (
	DefaultMaxIterations = 10
	DefaultTokenBudget   = 0 // 0 means unlimited.
	DefaultTimeout       = 5 * time.Minute
	DefaultLoopThreshold = 3
)

// LoopConfig controls the behavior of the agent reasoning loop.
type LoopConfig struct {
	// MaxIterations is the maximum number of reason-act cycles.
	MaxIterations int

	// TokenBudget is the cumulative token limit (input + output).
	// Zero means unlimited.
	TokenBudget int

	// Timeout is the maximum wall-clock duration for the loop.
	Timeout time.Duration

	// LoopThreshold is how many times the same tool call (name + args)
	// can repeat before the loop is considered stuck.
	LoopThreshold int
}

// withDefaults returns a copy with zero fields replaced by defaults.
func (c LoopConfig) withDefaults() LoopConfig {
	if c.MaxIterations <= 0 {
		c.MaxIterations = DefaultMaxIterations
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.LoopThreshold <= 0 {
		c.LoopThreshold = DefaultLoopThreshold
	}
	return c
}
