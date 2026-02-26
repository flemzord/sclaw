package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sensitiveEnvPrefixes are environment variable prefixes that are stripped
// from subprocess environments to prevent secret leakage.
// Entries here cover all variables with these prefixes; for variables that
// require exact matching only, see sensitiveEnvExact.
var sensitiveEnvPrefixes = []string{
	"OPENAI_",
	"ANTHROPIC_",
	"AWS_SECRET",
	"AWS_SESSION_TOKEN",
	"SLACK_TOKEN",
	"SLACK_BOT_TOKEN",
	"DISCORD_TOKEN",
	"TELEGRAM_BOT_TOKEN",
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"GITLAB_TOKEN",
	"SMTP_PASSWORD",
}

// sensitiveEnvExact are environment variable names that are stripped exactly.
// DATABASE_URL and DB_PASSWORD are exact-only to avoid over-blocking variables
// like DB_PORT or DATABASE_HOST which share the same prefix.
var sensitiveEnvExact = map[string]struct{}{
	"AWS_SECRET_ACCESS_KEY": {},
	"DATABASE_URL":          {},
	"DB_PASSWORD":           {},
	"REDIS_PASSWORD":        {},
}

// SanitizedEnv returns a copy of os.Environ() with sensitive environment
// variables removed. If store is non-nil, any credential values registered
// in it are also stripped from remaining variable values.
func SanitizedEnv(store *CredentialStore) []string {
	env := os.Environ()
	result := make([]string, 0, len(env))

	var secrets []string
	if store != nil {
		secrets = store.Values()
	}

	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}

		if isSensitiveEnvVar(key) {
			continue
		}

		// Redact credential values that might appear in remaining vars.
		// Only redact secrets of at least 8 characters to avoid false positives
		// from short values (e.g., "yes", "true", single letters).
		sanitized := entry
		for _, secret := range secrets {
			if secret != "" && len(secret) >= 8 && strings.Contains(sanitized, secret) {
				sanitized = strings.ReplaceAll(sanitized, secret, RedactPlaceholder)
			}
		}

		result = append(result, sanitized)
	}

	return result
}

// isSensitiveEnvVar checks if an environment variable name matches
// a known sensitive prefix or exact name.
func isSensitiveEnvVar(name string) bool {
	upper := strings.ToUpper(name)

	if _, ok := sensitiveEnvExact[upper]; ok {
		return true
	}

	for _, prefix := range sensitiveEnvPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}

	return false
}

// ErrRestrictedPath is returned when an attempt to access a restricted
// system path (/proc, /sys, /dev) is detected.
var ErrRestrictedPath = errors.New("access to restricted path is not allowed")

// ValidatePath checks that a filesystem path does not access sensitive
// system resources like /proc, /sys, or /dev which could leak secrets or
// allow device manipulation. The path is resolved to an absolute path and
// symlinks are followed (best-effort) before checking to prevent bypasses.
func ValidatePath(path string) error {
	cleaned := filepath.Clean(path)
	// Convert to absolute to catch relative paths like "proc/self/environ".
	abs, err := filepath.Abs(cleaned)
	if err == nil {
		cleaned = abs
	}
	// Resolve symlinks to prevent bypass via symlink traversal.
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err == nil {
		cleaned = resolved
	}
	normalized := strings.ToLower(cleaned)

	blockedPrefixes := []string{"/proc/", "/sys/", "/dev/"}
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return fmt.Errorf("%w: %s", ErrRestrictedPath, path)
		}
	}
	return nil
}

// EscapeShellArg escapes a string for safe use as a shell argument.
// It wraps the argument in single quotes and escapes any embedded
// single quotes using the standard shell escaping technique.
func EscapeShellArg(s string) string {
	// Replace each ' with '\'' (end quote, escaped quote, start quote).
	escaped := strings.ReplaceAll(s, "'", `'\''`)
	return "'" + escaped + "'"
}
