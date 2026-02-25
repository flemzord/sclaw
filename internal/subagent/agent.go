// Package subagent provides ephemeral sub-agent sessions with isolated state.
// Sub-agents run their own ReAct loops independently from the parent agent,
// enabling parallel task delegation without polluting the parent's context.
package subagent

import (
	"context"
	"sync"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
)

// Status represents the lifecycle state of a sub-agent.
type Status string

// Status constants for sub-agent lifecycle.
const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusTimeout   Status = "timeout"
	StatusKilled    Status = "killed"
)

// SubAgent represents an ephemeral agent session with isolated state.
type SubAgent struct {
	mu           sync.Mutex
	ID           string
	ParentID     string
	Status       Status
	SystemPrompt string
	History      []provider.LLMMessage
	Result       *agent.Response
	Error        error
	CreatedAt    time.Time
	FinishedAt   time.Time
	cancel       context.CancelFunc
}

// Snapshot returns a point-in-time copy of the sub-agent state.
// Safe to return to callers without holding the lock.
func (s *SubAgent) Snapshot() Snap {
	s.mu.Lock()
	defer s.mu.Unlock()

	history := make([]provider.LLMMessage, len(s.History))
	copy(history, s.History)

	snap := Snap{
		ID:           s.ID,
		ParentID:     s.ParentID,
		Status:       s.Status,
		SystemPrompt: s.SystemPrompt,
		History:      history,
		CreatedAt:    s.CreatedAt,
		FinishedAt:   s.FinishedAt,
	}

	if s.Result != nil {
		r := *s.Result
		snap.Result = &r
	}
	if s.Error != nil {
		snap.ErrorMsg = s.Error.Error()
	}

	return snap
}

// Snap is a point-in-time copy of a SubAgent, safe for concurrent reads.
type Snap struct {
	ID           string                `json:"id"`
	ParentID     string                `json:"parent_id"`
	Status       Status                `json:"status"`
	SystemPrompt string                `json:"system_prompt,omitempty"`
	History      []provider.LLMMessage `json:"history,omitempty"`
	Result       *agent.Response       `json:"result,omitempty"`
	ErrorMsg     string                `json:"error,omitempty"`
	CreatedAt    time.Time             `json:"created_at"`
	FinishedAt   time.Time             `json:"finished_at,omitempty"`
}
