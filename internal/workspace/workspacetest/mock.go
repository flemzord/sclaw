// Package workspacetest provides test doubles for the workspace package.
package workspacetest

import "github.com/flemzord/sclaw/internal/workspace"

// MockSoulProvider is a test double for workspace.SoulProvider.
type MockSoulProvider struct {
	LoadFunc func() (string, error)
}

// Compile-time interface check.
var _ workspace.SoulProvider = (*MockSoulProvider)(nil)

// Load delegates to the configured LoadFunc.
func (m *MockSoulProvider) Load() (string, error) {
	return m.LoadFunc()
}
