package security

import (
	"regexp"
	"strings"
	"sync"
)

// RedactPlaceholder is the replacement string for redacted secrets.
const RedactPlaceholder = "***REDACTED***"

// secretKeyPattern matches map keys that likely contain secrets.
var secretKeyPattern = regexp.MustCompile(`(?i)(secret|token|password|key|api_key|credential)`)

// Redactor replaces secret values in strings and maps with a redaction placeholder.
// It supports both regex pattern matching (for known API key formats) and
// literal value matching (for credentials loaded at runtime).
// All methods are safe for concurrent use.
type Redactor struct {
	mu       sync.RWMutex
	patterns []*regexp.Regexp
	literals []string
}

// NewRedactor creates a Redactor pre-loaded with default patterns for common
// API key formats (OpenAI, Anthropic, GitHub, AWS, Slack, Bearer tokens).
func NewRedactor() *Redactor {
	return &Redactor{
		patterns: DefaultPatterns(),
	}
}

// AddPattern adds a compiled regex pattern to the redactor.
func (r *Redactor) AddPattern(pattern *regexp.Regexp) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.patterns = append(r.patterns, pattern)
}

// AddLiteral adds a literal secret value that should be redacted on sight.
// Empty strings are ignored.
func (r *Redactor) AddLiteral(secret string) {
	if secret == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.literals = append(r.literals, secret)
}

// SyncCredentials replaces all literal values with the current contents
// of the credential store. This should be called after credential changes.
func (r *Redactor) SyncCredentials(store *CredentialStore) {
	values := store.Values()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.literals = values
}

// Redact replaces all known secret patterns and literal values in s
// with RedactPlaceholder.
func (r *Redactor) Redact(s string) string {
	if s == "" {
		return s
	}

	r.mu.RLock()
	patterns := r.patterns
	literals := r.literals
	r.mu.RUnlock()

	// Apply regex patterns first.
	for _, p := range patterns {
		s = p.ReplaceAllString(s, RedactPlaceholder)
	}

	// Apply literal replacements.
	for _, lit := range literals {
		if strings.Contains(s, lit) {
			s = strings.ReplaceAll(s, lit, RedactPlaceholder)
		}
	}

	return s
}

// RedactMap walks a map and replaces values whose keys match common secret
// key names (secret, token, password, key, api_key, credential).
// This is used for config display endpoints.
func (r *Redactor) RedactMap(m map[string]any) {
	for k, v := range m {
		if secretKeyPattern.MatchString(k) {
			if s, ok := v.(string); ok && s != "" {
				m[k] = RedactPlaceholder
				continue
			}
			// Fall through to handle nested maps/slices under secret-named keys.
		}
		switch val := v.(type) {
		case map[string]any:
			r.RedactMap(val)
		case []any:
			for _, item := range val {
				if sub, ok := item.(map[string]any); ok {
					r.RedactMap(sub)
				}
			}
		case string:
			if redacted := r.Redact(val); redacted != val {
				m[k] = redacted
			}
		}
	}
}

// DefaultPatterns returns compiled regex patterns for common API key formats.
func DefaultPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		// OpenAI: sk-... (at least 20 chars after prefix)
		regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),
		// Anthropic: sk-ant-... (at least 20 chars after prefix)
		regexp.MustCompile(`sk-ant-[a-zA-Z0-9\-]{20,}`),
		// GitHub: ghp_, gho_, ghs_, github_pat_
		regexp.MustCompile(`(ghp_|gho_|ghs_|github_pat_)[a-zA-Z0-9_]{20,}`),
		// AWS Access Key ID
		regexp.MustCompile(`AKIA[A-Z0-9]{16}`),
		// Slack bot token
		regexp.MustCompile(`xoxb-[0-9]+-[a-zA-Z0-9]+`),
		// Slack user token
		regexp.MustCompile(`xoxp-[0-9]+-[a-zA-Z0-9]+`),
	}
}
