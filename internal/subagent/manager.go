package subagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
)

// Sentinel errors for manager operations.
var (
	ErrRecursiveSpawn  = errors.New("subagent: sub-agents cannot spawn other sub-agents")
	ErrMaxConcurrent   = errors.New("subagent: maximum concurrent sub-agents reached")
	ErrNotFound        = errors.New("subagent: not found")
	ErrNotRunning      = errors.New("subagent: not running")
	ErrAlreadyFinished = errors.New("subagent: already finished")
)

// LoopFactory creates agent loops for sub-agents.
type LoopFactory interface {
	NewLoop(systemPrompt string) (*agent.Loop, error)
}

// ManagerConfig configures the sub-agent manager.
type ManagerConfig struct {
	MaxConcurrent  int
	DefaultTimeout time.Duration
	MaxHistory     int
	Logger         *slog.Logger
	LoopFactory    LoopFactory
	Now            func() time.Time
}

func (c ManagerConfig) withDefaults() ManagerConfig {
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 5
	}
	if c.DefaultTimeout <= 0 {
		c.DefaultTimeout = 5 * time.Minute
	}
	if c.MaxHistory <= 0 {
		c.MaxHistory = 50
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Manager orchestrates sub-agent lifecycle.
type Manager struct {
	cfg ManagerConfig

	mu     sync.Mutex
	agents map[string]*SubAgent
	active int
}

// NewManager creates a sub-agent manager.
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		cfg:    cfg.withDefaults(),
		agents: make(map[string]*SubAgent),
	}
}

// SpawnRequest is the input for spawning a sub-agent.
type SpawnRequest struct {
	ParentID       string
	SessionID      string // ID of the calling session for cross-session validation
	SystemPrompt   string
	InitialMessage string
	Timeout        time.Duration // 0 = use default
	IsSubAgent     bool          // true = reject (no recursive spawn)
}

// ErrCrossSession is returned when a spawn request comes from a different
// session than the parent agent, preventing cross-session data access.
var ErrCrossSession = errors.New("subagent: cross-session spawn not allowed")

// Spawn creates and starts a new sub-agent. Returns the sub-agent ID.
func (m *Manager) Spawn(ctx context.Context, req SpawnRequest) (string, error) {
	if req.IsSubAgent {
		return "", ErrRecursiveSpawn
	}

	if req.SessionID == "" {
		m.cfg.Logger.Warn("subagent: spawn without SessionID, cross-session validation disabled",
			"parent_id", req.ParentID,
		)
	}

	m.mu.Lock()

	// Cross-session validation: if a SessionID is provided on the request,
	// verify that any existing sub-agent for this ParentID belongs to the
	// same session. Performed under the same lock as insertion to prevent
	// TOCTOU race conditions.
	if req.SessionID != "" && req.ParentID != "" {
		for _, sa := range m.agents {
			if sa.ParentID == req.ParentID && sa.SessionID != "" && sa.SessionID != req.SessionID {
				m.mu.Unlock()
				return "", ErrCrossSession
			}
		}
	}

	if m.active >= m.cfg.MaxConcurrent {
		m.mu.Unlock()
		return "", ErrMaxConcurrent
	}

	id, err := generateID()
	if err != nil {
		m.mu.Unlock()
		return "", fmt.Errorf("subagent: failed to generate ID: %w", err)
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = m.cfg.DefaultTimeout
	}

	agentCtx, cancel := context.WithTimeout(ctx, timeout)

	sa := &SubAgent{
		ID:           id,
		ParentID:     req.ParentID,
		SessionID:    req.SessionID,
		Status:       StatusRunning,
		SystemPrompt: req.SystemPrompt,
		History: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: req.InitialMessage},
		},
		CreatedAt: m.cfg.Now(),
		cancel:    cancel,
	}

	m.agents[id] = sa
	m.active++
	m.mu.Unlock()

	loop, err := m.cfg.LoopFactory.NewLoop(req.SystemPrompt)
	if err != nil {
		cancel()
		m.mu.Lock()
		sa.mu.Lock()
		sa.Status = StatusFailed
		sa.Error = err
		sa.FinishedAt = m.cfg.Now()
		sa.mu.Unlock()
		m.active--
		m.mu.Unlock()
		// Return ID even on factory failure so caller can check history.
		return id, nil
	}

	go m.runAgent(agentCtx, sa, loop, req.InitialMessage)

	return id, nil
}

// runAgent executes the agent loop in a goroutine.
func (m *Manager) runAgent(ctx context.Context, sa *SubAgent, loop *agent.Loop, initialMessage string) {
	defer func() {
		m.mu.Lock()
		m.active--
		m.mu.Unlock()
	}()

	resp, err := loop.Run(ctx, agent.Request{
		SystemPrompt: sa.SystemPrompt,
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: initialMessage},
		},
	})

	sa.mu.Lock()
	defer sa.mu.Unlock()

	// Don't overwrite killed status.
	if sa.Status == StatusKilled {
		return
	}

	sa.FinishedAt = m.cfg.Now()

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			sa.Status = StatusTimeout
		} else {
			sa.Status = StatusFailed
		}
		sa.Error = err
		sa.Result = &resp
		return
	}

	sa.Status = StatusCompleted
	sa.Result = &resp

	// Append assistant response to history.
	if resp.Content != "" {
		sa.History = append(sa.History, provider.LLMMessage{
			Role:    provider.MessageRoleAssistant,
			Content: resp.Content,
		})
		// Enforce history cap.
		if len(sa.History) > m.cfg.MaxHistory {
			sa.History = sa.History[len(sa.History)-m.cfg.MaxHistory:]
		}
	}
}

// Send sends a message to a running sub-agent and appends it to history.
func (m *Manager) Send(_ context.Context, id, message string) error {
	m.mu.Lock()
	sa, ok := m.agents[id]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	sa.mu.Lock()
	defer sa.mu.Unlock()

	if sa.Status != StatusRunning {
		return fmt.Errorf("%w: %s (status: %s)", ErrNotRunning, id, sa.Status)
	}

	// Append user message to history.
	sa.History = append(sa.History, provider.LLMMessage{
		Role:    provider.MessageRoleUser,
		Content: message,
	})

	// NOTE: In a full implementation, this would run another agent loop iteration.
	// For now, the sub-agent processes messages through the initial loop.
	// A more complete Send would require the sub-agent to have a message channel.
	return nil
}

// List returns snapshots of all sub-agents with the given parent.
func (m *Manager) List(parentID string) []Snap {
	m.mu.Lock()
	defer m.mu.Unlock()

	var snapshots []Snap
	for _, sa := range m.agents {
		if sa.ParentID == parentID {
			snapshots = append(snapshots, sa.Snapshot())
		}
	}
	return snapshots
}

// History returns the snapshot for a specific sub-agent.
func (m *Manager) History(id string) (Snap, error) {
	m.mu.Lock()
	sa, ok := m.agents[id]
	m.mu.Unlock()

	if !ok {
		return Snap{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	return sa.Snapshot(), nil
}

// Kill terminates a running sub-agent.
func (m *Manager) Kill(id string) error {
	m.mu.Lock()
	sa, ok := m.agents[id]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	sa.mu.Lock()
	defer sa.mu.Unlock()

	if sa.Status != StatusRunning {
		return fmt.Errorf("%w: %s (status: %s)", ErrAlreadyFinished, id, sa.Status)
	}

	sa.Status = StatusKilled
	sa.FinishedAt = m.cfg.Now()
	if sa.cancel != nil {
		sa.cancel()
	}

	return nil
}

// Shutdown kills all running sub-agents.
func (m *Manager) Shutdown(_ context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, sa := range m.agents {
		sa.mu.Lock()
		if sa.Status == StatusRunning {
			sa.Status = StatusKilled
			sa.FinishedAt = m.cfg.Now()
			if sa.cancel != nil {
				sa.cancel()
			}
		}
		sa.mu.Unlock()
	}
}

// generateID produces a 32-character hex string from 16 random bytes.
func generateID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("subagent: crypto/rand unavailable: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
