// Package router dispatches inbound messages to agent sessions,
// managing session lifecycle, per-session serialization, and routing.
package router

import "errors"

// Sentinel errors for router operations.
var (
	// ErrInboxFull indicates the router's message inbox is at capacity
	// and the incoming message was dropped. Callers should back off or
	// alert the operator.
	ErrInboxFull = errors.New("router: inbox full, message dropped")

	// ErrRouterStopped indicates the router has been shut down and is
	// no longer accepting messages.
	ErrRouterStopped = errors.New("router: stopped")

	// ErrNoAgentFactory indicates no agent factory has been configured.
	// The router cannot create agent instances without one.
	ErrNoAgentFactory = errors.New("router: no agent factory configured")

	// ErrNoResponseSender indicates no response sender has been configured.
	// The router cannot deliver outbound messages without one.
	ErrNoResponseSender = errors.New("router: no response sender configured")
)
