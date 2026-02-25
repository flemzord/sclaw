package gateway

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// authMiddleware returns a chi-compatible middleware that validates Bearer token
// or Basic auth credentials using constant-time comparison.
func authMiddleware(cfg AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Try Bearer token first.
			if cfg.BearerToken != "" {
				if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
					if constantTimeEqual(after, cfg.BearerToken) {
						next.ServeHTTP(w, r)
						return
					}
				}
			}

			// Try Basic auth.
			if cfg.BasicUser != "" && cfg.BasicPass != "" {
				user, pass, ok := r.BasicAuth()
				if ok && constantTimeEqual(user, cfg.BasicUser) && constantTimeEqual(pass, cfg.BasicPass) {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
}

// constantTimeEqual compares two strings in constant time.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
