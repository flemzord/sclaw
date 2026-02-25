package multiagent

import "errors"

var (
	// ErrNoMatchingAgent is returned when no agent matches the inbound message.
	ErrNoMatchingAgent = errors.New("multiagent: no agent matches the message")
	// ErrDuplicateDefault is returned when multiple agents are marked as default.
	ErrDuplicateDefault = errors.New("multiagent: multiple agents marked as default")
	// ErrAgentNotFound is returned when a requested agent ID does not exist.
	ErrAgentNotFound = errors.New("multiagent: agent not found")
)
