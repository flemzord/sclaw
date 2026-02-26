package security

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrURLBlocked is returned when a URL is denied by the filter.
var ErrURLBlocked = errors.New("URL blocked by filter")

// URLFilterConfig holds the configuration for URL filtering.
type URLFilterConfig struct {
	// AllowDomains is the list of allowed domains. If empty, ALL domains
	// are blocked (default-deny). Subdomains are matched: allowing
	// "example.com" also allows "api.example.com".
	AllowDomains []string `yaml:"allow_domains"`

	// DenyDomains is the list of explicitly denied domains. Deny takes
	// precedence over allow. Used to block specific subdomains of allowed
	// domains.
	DenyDomains []string `yaml:"deny_domains"`
}

// URLFilter implements default-deny URL filtering with allow/deny domain lists.
type URLFilter struct {
	allow []string
	deny  []string
}

// NewURLFilter creates a URL filter from the given config.
func NewURLFilter(cfg URLFilterConfig) *URLFilter {
	// Normalize domains to lowercase.
	allow := make([]string, len(cfg.AllowDomains))
	for i, d := range cfg.AllowDomains {
		allow[i] = strings.ToLower(strings.TrimSpace(d))
	}
	deny := make([]string, len(cfg.DenyDomains))
	for i, d := range cfg.DenyDomains {
		deny[i] = strings.ToLower(strings.TrimSpace(d))
	}
	return &URLFilter{allow: allow, deny: deny}
}

// Check validates that the URL is allowed by the filter.
// Returns nil if allowed, ErrURLBlocked if denied.
func (f *URLFilter) Check(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: invalid URL: %w", ErrURLBlocked, err)
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("%w: empty hostname", ErrURLBlocked)
	}

	// Deny list takes precedence.
	for _, d := range f.deny {
		if matchDomain(host, d) {
			return fmt.Errorf("%w: %s (denied)", ErrURLBlocked, host)
		}
	}

	// If no allow list is configured, block everything (default-deny).
	if len(f.allow) == 0 {
		return fmt.Errorf("%w: %s (no domains allowed)", ErrURLBlocked, host)
	}

	// Check allow list.
	for _, a := range f.allow {
		if matchDomain(host, a) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s (not in allow list)", ErrURLBlocked, host)
}

// IsConfigured returns true if any allow or deny domains are configured.
func (f *URLFilter) IsConfigured() bool {
	return len(f.allow) > 0 || len(f.deny) > 0
}

// matchDomain checks if host matches domain or is a subdomain of it.
// "api.example.com" matches "example.com".
// "example.com" matches "example.com".
// "notexample.com" does NOT match "example.com".
func matchDomain(host, domain string) bool {
	if host == domain {
		return true
	}
	return strings.HasSuffix(host, "."+domain)
}
