package anthropic

import (
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

// isContextLengthError checks whether a 400 error is specifically about
// exceeding the model's context window.
func isContextLengthError(apiErr *sdkanthropic.Error) bool {
	raw := apiErr.RawJSON()
	return strings.Contains(raw, "context length") ||
		strings.Contains(raw, "too many tokens") ||
		strings.Contains(raw, "token limit")
}
