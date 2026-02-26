package multiagent

import (
	"fmt"
	"slices"

	"github.com/flemzord/sclaw/pkg/message"
)

type agentEntry struct {
	ID     string
	Config AgentConfig
	Order  int
}

// Registry is an immutable resolution registry that maps inbound messages to agents.
// Resolution cascade: user -> group -> channel -> default -> error.
type Registry struct {
	agents       []agentEntry
	userIndex    map[string][]int // sender ID -> agent entry indices
	groupIndex   map[string][]int // chat ID -> agent entry indices
	channelIndex map[string][]int // channel name -> agent entry indices
	defaultAgent string
}

// NewRegistry builds an immutable Registry from the given agent configs and declaration order.
func NewRegistry(agents map[string]AgentConfig, order []string) (*Registry, error) {
	r := &Registry{
		userIndex:    make(map[string][]int),
		groupIndex:   make(map[string][]int),
		channelIndex: make(map[string][]int),
	}

	for i, id := range order {
		cfg := agents[id]
		entry := agentEntry{ID: id, Config: cfg, Order: i}
		r.agents = append(r.agents, entry)
		idx := len(r.agents) - 1

		for _, u := range cfg.Routing.Users {
			r.userIndex[u] = append(r.userIndex[u], idx)
		}
		for _, g := range cfg.Routing.Groups {
			r.groupIndex[g] = append(r.groupIndex[g], idx)
		}
		for _, ch := range cfg.Routing.Channels {
			r.channelIndex[ch] = append(r.channelIndex[ch], idx)
		}

		if cfg.Routing.Default {
			if r.defaultAgent != "" {
				return nil, fmt.Errorf("%w: %q and %q", ErrDuplicateDefault, r.defaultAgent, id)
			}
			r.defaultAgent = id
		}
	}

	return r, nil
}

// Resolve returns the agent ID that should handle the given inbound message.
// It follows the cascade: user -> group -> channel -> default -> ErrNoMatchingAgent.
func (r *Registry) Resolve(msg message.InboundMessage) (string, error) {
	// Priority 1: user filter
	if indices, ok := r.userIndex[msg.Sender.ID]; ok && len(indices) > 0 {
		return r.agents[indices[0]].ID, nil
	}
	// Priority 2: group filter
	if indices, ok := r.groupIndex[msg.Chat.ID]; ok && len(indices) > 0 {
		return r.agents[indices[0]].ID, nil
	}
	// Priority 3: channel filter
	if indices, ok := r.channelIndex[msg.Channel]; ok && len(indices) > 0 {
		return r.agents[indices[0]].ID, nil
	}
	// Priority 4: default
	if r.defaultAgent != "" {
		return r.defaultAgent, nil
	}
	return "", ErrNoMatchingAgent
}

// AgentConfig returns the configuration for the agent with the given ID.
func (r *Registry) AgentConfig(id string) (AgentConfig, bool) {
	idx := slices.IndexFunc(r.agents, func(e agentEntry) bool {
		return e.ID == id
	})
	if idx < 0 {
		return AgentConfig{}, false
	}
	return r.agents[idx].Config, true
}

// AgentIDs returns all registered agent IDs in declaration order.
func (r *Registry) AgentIDs() []string {
	ids := make([]string, len(r.agents))
	for i, e := range r.agents {
		ids[i] = e.ID
	}
	return ids
}
