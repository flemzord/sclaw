package security

import (
	"errors"
	"testing"
)

func TestURLFilter_DefaultDeny(t *testing.T) {
	t.Parallel()

	f := NewURLFilter(URLFilterConfig{})

	if err := f.Check("https://example.com"); !errors.Is(err, ErrURLBlocked) {
		t.Errorf("expected ErrURLBlocked, got %v", err)
	}
}

func TestURLFilter_AllowList(t *testing.T) {
	t.Parallel()

	f := NewURLFilter(URLFilterConfig{
		AllowDomains: []string{"example.com", "api.github.com"},
	})

	tests := []struct {
		url     string
		allowed bool
	}{
		{"https://example.com/path", true},
		{"https://api.example.com/v1", true},
		{"https://api.github.com/repos", true},
		{"https://evil.com", false},
		{"https://notexample.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel()
			err := f.Check(tt.url)
			if tt.allowed && err != nil {
				t.Errorf("expected allow, got %v", err)
			}
			if !tt.allowed && !errors.Is(err, ErrURLBlocked) {
				t.Errorf("expected ErrURLBlocked, got %v", err)
			}
		})
	}
}

func TestURLFilter_DenyTakesPrecedence(t *testing.T) {
	t.Parallel()

	f := NewURLFilter(URLFilterConfig{
		AllowDomains: []string{"example.com"},
		DenyDomains:  []string{"evil.example.com"},
	})

	// Regular subdomain should be allowed.
	if err := f.Check("https://api.example.com"); err != nil {
		t.Errorf("api.example.com should be allowed: %v", err)
	}

	// Denied subdomain should be blocked.
	if err := f.Check("https://evil.example.com"); !errors.Is(err, ErrURLBlocked) {
		t.Errorf("evil.example.com should be blocked: %v", err)
	}
}

func TestURLFilter_InvalidURL(t *testing.T) {
	t.Parallel()

	f := NewURLFilter(URLFilterConfig{AllowDomains: []string{"example.com"}})

	if err := f.Check("://invalid"); !errors.Is(err, ErrURLBlocked) {
		t.Errorf("expected ErrURLBlocked for invalid URL, got %v", err)
	}
}

func TestURLFilter_EmptyHostname(t *testing.T) {
	t.Parallel()

	f := NewURLFilter(URLFilterConfig{AllowDomains: []string{"example.com"}})

	if err := f.Check("/relative/path"); !errors.Is(err, ErrURLBlocked) {
		t.Errorf("expected ErrURLBlocked for empty hostname, got %v", err)
	}
}

func TestURLFilter_CaseInsensitive(t *testing.T) {
	t.Parallel()

	f := NewURLFilter(URLFilterConfig{
		AllowDomains: []string{"Example.COM"},
	})

	if err := f.Check("https://EXAMPLE.com/path"); err != nil {
		t.Errorf("case-insensitive match failed: %v", err)
	}
}

func TestURLFilter_IsConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  URLFilterConfig
		want bool
	}{
		{"empty", URLFilterConfig{}, false},
		{"allow only", URLFilterConfig{AllowDomains: []string{"a.com"}}, true},
		{"deny only", URLFilterConfig{DenyDomains: []string{"b.com"}}, true},
		{"both", URLFilterConfig{AllowDomains: []string{"a.com"}, DenyDomains: []string{"b.com"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := NewURLFilter(tt.cfg)
			if got := f.IsConfigured(); got != tt.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		host   string
		domain string
		want   bool
	}{
		{"example.com", "example.com", true},
		{"api.example.com", "example.com", true},
		{"deep.api.example.com", "example.com", true},
		{"notexample.com", "example.com", false},
		{"example.com.evil.com", "example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.host+"_"+tt.domain, func(t *testing.T) {
			t.Parallel()
			got := matchDomain(tt.host, tt.domain)
			if got != tt.want {
				t.Errorf("matchDomain(%q, %q) = %v, want %v", tt.host, tt.domain, got, tt.want)
			}
		})
	}
}
