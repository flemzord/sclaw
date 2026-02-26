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
var sensitiveEnvPrefixes = []string{
	"OPENAI_",
	"ANTHROPIC_",
	"AWS_SECRET",
	"AWS_SESSION_TOKEN",
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"GITLAB_TOKEN",
	"SLACK_TOKEN",
	"SLACK_BOT_TOKEN",
	"DISCORD_TOKEN",
	"TELEGRAM_BOT_TOKEN",
	"DATABASE_URL",
	"DB_PASSWORD",
	"REDIS_PASSWORD",
	"SMTP_PASSWORD",
}

// sensitiveEnvExact are environment variable names that are stripped exactly.
var sensitiveEnvExact = map[string]struct{}{
	"AWS_SECRET_ACCESS_KEY": {},
	"GITHUB_TOKEN":          {},
	"GH_TOKEN":              {},
	"GITLAB_TOKEN":          {},
	"SLACK_TOKEN":           {},
	"SLACK_BOT_TOKEN":       {},
	"DISCORD_TOKEN":         {},
	"TELEGRAM_BOT_TOKEN":    {},
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
		sanitized := entry
		for _, secret := range secrets {
			if secret != "" && strings.Contains(sanitized, secret) {
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

// ErrProcAccess is returned when an attempt to access /proc is detected.
var ErrProcAccess = errors.New("access to /proc is not allowed")

// ValidatePath checks that a filesystem path does not access sensitive
// system resources like /proc/self/environ which could leak secrets.
// The path is cleaned and normalized before checking to prevent bypasses
// via relative paths or directory traversal (e.g., "../proc/self/environ").
func ValidatePath(path string) error {
	// Clean to resolve "..", ".", and redundant slashes before checking.
	cleaned := filepath.Clean(path)
	normalized := strings.ToLower(cleaned)

	// Block /proc/*/environ and /proc/self/* entirely.
	if strings.HasPrefix(normalized, "/proc/") {
		if strings.Contains(normalized, "/environ") {
			return fmt.Errorf("%w: %s", ErrProcAccess, path)
		}
		if strings.Contains(normalized, "/self/") {
			return fmt.Errorf("%w: %s (self access blocked)", ErrProcAccess, path)
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
