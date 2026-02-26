package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdkanthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/flemzord/sclaw/internal/provider"
)

// mapError converts an Anthropic SDK error into the appropriate provider
// sentinel error. Non-API errors are returned as-is.
func mapError(err error) error {
	if err == nil {
		return nil
	}

	// Surface context errors directly so the provider chain recognises
	// them as non-retryable without unnecessary failover attempts.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	var apiErr *sdkanthropic.Error
	if !errors.As(err, &apiErr) {
		return err
	}

	switch apiErr.StatusCode {
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", provider.ErrRateLimit, apiErr.Error())
	case 529, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
		return fmt.Errorf("%w: %s", provider.ErrProviderDown, apiErr.Error())
	case http.StatusBadRequest:
		if isContextLengthError(apiErr) {
			return fmt.Errorf("%w: %s", provider.ErrContextLength, apiErr.Error())
		}
		return fmt.Errorf("anthropic bad request: %w", err)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("anthropic auth error (HTTP %d): %w", apiErr.StatusCode, err)
	default:
		return fmt.Errorf("anthropic error (HTTP %d): %w", apiErr.StatusCode, err)
	}
}

// apiErrorBody is a minimal representation of the Anthropic error JSON
// used for structured detection of specific error types.
type apiErrorBody struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// isContextLengthError checks whether a 400 error is specifically about
// exceeding the model's context window. It first verifies the structured
// error type, then falls back to message substring matching.
func isContextLengthError(apiErr *sdkanthropic.Error) bool {
	raw := apiErr.RawJSON()

	var body apiErrorBody
	if err := json.Unmarshal([]byte(raw), &body); err == nil {
		// Only classify as context-length if the error type is the expected one.
		if body.Error.Type != "invalid_request_error" {
			return false
		}
		msg := body.Error.Message
		return strings.Contains(msg, "context length") ||
			strings.Contains(msg, "too many tokens") ||
			strings.Contains(msg, "token limit")
	}

	// Fallback: unstructured matching (should not happen with a well-formed API).
	return strings.Contains(raw, "context length") ||
		strings.Contains(raw, "too many tokens") ||
		strings.Contains(raw, "token limit")
}
