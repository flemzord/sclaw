package agent

import (
	"testing"
	"time"
)

func TestWithDefaults_ZeroValue(t *testing.T) {
	t.Parallel()

	cfg := LoopConfig{}.withDefaults()

	if cfg.MaxIterations != DefaultMaxIterations {
		t.Errorf("MaxIterations = %d, want %d", cfg.MaxIterations, DefaultMaxIterations)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, DefaultTimeout)
	}
	if cfg.LoopThreshold != DefaultLoopThreshold {
		t.Errorf("LoopThreshold = %d, want %d", cfg.LoopThreshold, DefaultLoopThreshold)
	}
	if cfg.TokenBudget != 0 {
		t.Errorf("TokenBudget = %d, want 0 (unlimited)", cfg.TokenBudget)
	}
}

func TestWithDefaults_ExplicitValues(t *testing.T) {
	t.Parallel()

	cfg := LoopConfig{
		MaxIterations: 20,
		TokenBudget:   5000,
		Timeout:       10 * time.Minute,
		LoopThreshold: 5,
	}.withDefaults()

	if cfg.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d, want 20", cfg.MaxIterations)
	}
	if cfg.TokenBudget != 5000 {
		t.Errorf("TokenBudget = %d, want 5000", cfg.TokenBudget)
	}
	if cfg.Timeout != 10*time.Minute {
		t.Errorf("Timeout = %v, want 10m", cfg.Timeout)
	}
	if cfg.LoopThreshold != 5 {
		t.Errorf("LoopThreshold = %d, want 5", cfg.LoopThreshold)
	}
}

func TestWithDefaults_ZeroBudgetStaysUnlimited(t *testing.T) {
	t.Parallel()

	cfg := LoopConfig{
		MaxIterations: 5,
		TokenBudget:   0,
	}.withDefaults()

	if cfg.TokenBudget != 0 {
		t.Errorf("TokenBudget = %d, want 0 (unlimited)", cfg.TokenBudget)
	}
}
