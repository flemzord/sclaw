package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/flemzord/sclaw/internal/provider"
)

// errAuth is a non-retryable authentication error.
var errAuth = errors.New("openai: authentication failed")

// mapHTTPError maps an HTTP status code and response body to a provider
// sentinel error. Returns nil for 2xx status codes.
func mapHTTPError(statusCode int, body []byte) error {
	if statusCode >= 200 && statusCode < 300 {
		return nil
	}

	// Try to extract the error message from the response body.
	var msg string
	var apiErr apiError
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
		msg = apiErr.Error.Message
	} else {
		msg = string(body)
	}

	switch {
	case statusCode == 429:
		return fmt.Errorf("%w: %s", provider.ErrRateLimit, msg)
	case statusCode == 401 || statusCode == 403:
		return fmt.Errorf("%w: %s", errAuth, msg)
	case statusCode == 400 && strings.Contains(strings.ToLower(msg), "context_length"):
		return fmt.Errorf("%w: %s", provider.ErrContextLength, msg)
	case statusCode >= 500:
		return fmt.Errorf("%w: %s", provider.ErrProviderDown, msg)
	default:
		return fmt.Errorf("openai: HTTP %d: %s", statusCode, msg)
	}
}

// mapConnectionError maps network-level errors to provider sentinel errors.
// Context errors pass through unchanged.
func mapConnectionError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%w: %w", provider.ErrProviderDown, err)
	}
	return fmt.Errorf("openai: %w", err)
}
