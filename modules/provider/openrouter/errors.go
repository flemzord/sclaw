package openrouter

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/flemzord/sclaw/internal/provider"
)

// apiError represents an error response from the OpenRouter API.
type apiError struct {
	Error struct {
		Message string `json:"message"`
		Code    any    `json:"code"` // Can be string or int depending on upstream.
	} `json:"error"`
}

// mapHTTPError converts an HTTP status code and response body into the
// appropriate provider sentinel error.
func mapHTTPError(statusCode int, body io.Reader) error {
	var ae apiError

	data, readErr := io.ReadAll(io.LimitReader(body, 4096))
	if readErr == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &ae)
	}

	msg := ae.Error.Message
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", statusCode)
	}

	switch {
	case statusCode == 429:
		return fmt.Errorf("openrouter: %s: %w", msg, provider.ErrRateLimit)
	case statusCode == 401 || statusCode == 403:
		return fmt.Errorf("openrouter: %s", msg)
	case statusCode == 400 && isContextLengthError(msg):
		return fmt.Errorf("openrouter: %s: %w", msg, provider.ErrContextLength)
	case statusCode == 400:
		return fmt.Errorf("openrouter: %s", msg)
	case statusCode >= 500:
		return fmt.Errorf("openrouter: %s: %w", msg, provider.ErrProviderDown)
	default:
		return fmt.Errorf("openrouter: %s", msg)
	}
}

// mapAPIError converts an in-stream API error into a provider error.
func mapAPIError(ae apiError) error {
	msg := ae.Error.Message
	if msg == "" {
		msg = "unknown error"
	}

	lmsg := strings.ToLower(msg)
	switch {
	case strings.Contains(lmsg, "rate limit"):
		return fmt.Errorf("openrouter: %s: %w", msg, provider.ErrRateLimit)
	case isContextLengthError(msg):
		return fmt.Errorf("openrouter: %s: %w", msg, provider.ErrContextLength)
	default:
		return fmt.Errorf("openrouter: %s: %w", msg, provider.ErrProviderDown)
	}
}

// isContextLengthError checks whether an error message indicates a context
// length overflow.
func isContextLengthError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "context length") ||
		strings.Contains(lower, "context_length") ||
		strings.Contains(lower, "maximum context") ||
		strings.Contains(lower, "token limit")
}
