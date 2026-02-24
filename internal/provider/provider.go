package provider

import "context"

// Provider is the interface for communicating with an LLM.
// Concrete implementations live in separate packages (e.g., provider.openai)
// and typically also implement core.Module for lifecycle management.
type Provider interface {
	// Complete sends a completion request and returns the full response.
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)

	// Stream sends a completion request and returns a channel of chunks.
	// Initial connection errors are returned directly. Mid-stream errors
	// are delivered via StreamChunk.Err.
	Stream(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error)

	// ContextWindowSize returns the maximum context window in tokens.
	ContextWindowSize() int

	// ModelName returns the identifier of the underlying model.
	ModelName() string
}

// HealthChecker is an optional interface that providers may implement
// to support active health probing. When a provider is in cooldown,
// the health tracker will call HealthCheck periodically to determine
// if the provider has recovered.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}
