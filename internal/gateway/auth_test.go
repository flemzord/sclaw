package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestAuthMiddleware_ValidBearerToken(t *testing.T) {
	t.Parallel()

	cfg := AuthConfig{BearerToken: "secret-token"}
	handler := authMiddleware(cfg, nil, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_InvalidBearerToken(t *testing.T) {
	t.Parallel()

	cfg := AuthConfig{BearerToken: "secret-token"}
	handler := authMiddleware(cfg, nil, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_ValidBasicAuth(t *testing.T) {
	t.Parallel()

	cfg := AuthConfig{BasicUser: "admin", BasicPass: "pass123"}
	handler := authMiddleware(cfg, nil, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "pass123")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_InvalidBasicAuth(t *testing.T) {
	t.Parallel()

	cfg := AuthConfig{BasicUser: "admin", BasicPass: "pass123"}
	handler := authMiddleware(cfg, nil, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "wrongpass")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_NoAuthHeader(t *testing.T) {
	t.Parallel()

	cfg := AuthConfig{BearerToken: "token"}
	handler := authMiddleware(cfg, nil, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_BearerPreferredOverBasic(t *testing.T) {
	t.Parallel()

	cfg := AuthConfig{
		BearerToken: "my-token",
		BasicUser:   "admin",
		BasicPass:   "pass",
	}
	handler := authMiddleware(cfg, nil, nil)(okHandler())

	// Bearer should work
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer my-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("bearer: status = %d, want %d", rr.Code, http.StatusOK)
	}

	// Basic should also work
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.SetBasicAuth("admin", "pass")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("basic: status = %d, want %d", rr2.Code, http.StatusOK)
	}
}

func TestAuthConfig_IsConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  AuthConfig
		want bool
	}{
		{"empty", AuthConfig{}, false},
		{"bearer only", AuthConfig{BearerToken: "tok"}, true},
		{"basic complete", AuthConfig{BasicUser: "u", BasicPass: "p"}, true},
		{"basic partial user", AuthConfig{BasicUser: "u"}, false},
		{"basic partial pass", AuthConfig{BasicPass: "p"}, false},
		{"both", AuthConfig{BearerToken: "t", BasicUser: "u", BasicPass: "p"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.cfg.IsConfigured(); got != tt.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}
