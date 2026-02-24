// Package providertest provides test helpers for the provider package.
package providertest

import (
	"context"
	"sync"

	"github.com/flemzord/sclaw/internal/provider"
)

// MockProvider is a configurable test double for provider.Provider.
// Set the Func fields to control behavior. Unset funcs panic on call.
// All methods are safe for concurrent use.
type MockProvider struct {
	CompleteFunc          func(ctx context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error)
	StreamFunc            func(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamChunk, error)
	ContextWindowSizeFunc func() int
	ModelNameFunc         func() string
	HealthCheckFunc       func(ctx context.Context) error

	mu            sync.Mutex
	CompleteCalls int
	StreamCalls   int
	HealthCalls   int
}

// Complete delegates to CompleteFunc and tracks call count.
func (m *MockProvider) Complete(ctx context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	m.mu.Lock()
	m.CompleteCalls++
	m.mu.Unlock()
	return m.CompleteFunc(ctx, req)
}

// Stream delegates to StreamFunc and tracks call count.
func (m *MockProvider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	m.mu.Lock()
	m.StreamCalls++
	m.mu.Unlock()
	return m.StreamFunc(ctx, req)
}

// ContextWindowSize delegates to ContextWindowSizeFunc.
func (m *MockProvider) ContextWindowSize() int {
	return m.ContextWindowSizeFunc()
}

// ModelName delegates to ModelNameFunc.
func (m *MockProvider) ModelName() string {
	return m.ModelNameFunc()
}

// HealthCheck delegates to HealthCheckFunc and tracks call count.
func (m *MockProvider) HealthCheck(ctx context.Context) error {
	m.mu.Lock()
	m.HealthCalls++
	m.mu.Unlock()
	return m.HealthCheckFunc(ctx)
}

// Interface guards.
var (
	_ provider.Provider      = (*MockProvider)(nil)
	_ provider.HealthChecker = (*MockProvider)(nil)
)
