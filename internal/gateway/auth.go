package gateway

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/flemzord/sclaw/internal/security"
)

// authMiddleware returns a chi-compatible middleware that validates Bearer token
// or Basic auth credentials using constant-time comparison.
// If an AuditLogger is provided, auth_success and auth_failure events are emitted.
// If a RateLimiter is provided, auth attempts are rate-limited using the "auth" bucket.
func authMiddleware(cfg AuthConfig, auditLogger *security.AuditLogger, rateLimiter *security.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Rate limit auth attempts using the central rate limiter.
			if rateLimiter != nil {
				if err := rateLimiter.Allow("auth"); err != nil {
					http.Error(w, "too many requests", http.StatusTooManyRequests)
					return
				}
			}

			auth := r.Header.Get("Authorization")
			if auth == "" {
				emitAuthEvent(auditLogger, security.EventAuthFailure, r, "missing authorization header")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Try Bearer token first.
			if cfg.BearerToken != "" {
				if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
					if constantTimeEqual(after, cfg.BearerToken) {
						emitAuthEvent(auditLogger, security.EventAuthSuccess, r, "bearer")
						next.ServeHTTP(w, r)
						return
					}
				}
			}

			// Try Basic auth.
			if cfg.BasicUser != "" && cfg.BasicPass != "" {
				user, pass, ok := r.BasicAuth()
				if ok && constantTimeEqual(user, cfg.BasicUser) && constantTimeEqual(pass, cfg.BasicPass) {
					emitAuthEvent(auditLogger, security.EventAuthSuccess, r, "basic")
					next.ServeHTTP(w, r)
					return
				}
			}

			emitAuthEvent(auditLogger, security.EventAuthFailure, r, "invalid credentials")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
}

// emitAuthEvent logs an auth event to the audit logger if available.
func emitAuthEvent(logger *security.AuditLogger, eventType security.EventType, r *http.Request, detail string) {
	if logger == nil {
		return
	}
	logger.Log(security.AuditEvent{
		Type:   eventType,
		Detail: detail,
		Metadata: map[string]string{
			"remote_addr": r.RemoteAddr,
			"method":      r.Method,
			"path":        r.URL.Path,
		},
	})
}

// constantTimeEqual compares two strings in constant time.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
