// Package securitytest provides test doubles for the security package.
// It is intended for use by other packages' tests, following the
// providertest pattern established in the codebase.
package securitytest

import (
	"github.com/flemzord/sclaw/internal/security"
)

// NewTestRedactor creates a Redactor with no patterns for testing.
// This avoids false positives in tests that use strings matching
// production secret patterns.
// Direct instantiation is safe because sync.RWMutex zero-value is valid
// and nil slices work correctly with range/append operations.
func NewTestRedactor() *security.Redactor {
	return &security.Redactor{}
}

// NewTestCredentialStore creates a CredentialStore pre-populated with
// the given key-value pairs. Panics if an odd number of args is provided.
func NewTestCredentialStore(kvs ...string) *security.CredentialStore {
	if len(kvs)%2 != 0 {
		panic("securitytest: NewTestCredentialStore requires even number of args (key, value pairs)")
	}
	store := security.NewCredentialStore()
	for i := 0; i < len(kvs); i += 2 {
		store.Set(kvs[i], kvs[i+1])
	}
	return store
}

// NewTestAuditLogger creates an AuditLogger that writes to a buffer
// for test inspection. Returns the logger and a function to retrieve
// logged events.
func NewTestAuditLogger() (*security.AuditLogger, func() []security.AuditEvent) {
	var events []security.AuditEvent
	logger := security.NewAuditLogger(security.AuditLoggerConfig{
		OnEvent: func(e security.AuditEvent) {
			events = append(events, e)
		},
	})
	return logger, func() []security.AuditEvent {
		return events
	}
}
