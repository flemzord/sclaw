package provider

import "errors"

// Sentinel errors for provider operations.
var (
	// ErrRateLimit indicates the provider returned a rate limit response.
	ErrRateLimit = errors.New("provider rate limited")

	// ErrContextLength indicates the request exceeded the model's context window.
	ErrContextLength = errors.New("context length exceeded")

	// ErrProviderDown indicates the provider is temporarily unavailable.
	ErrProviderDown = errors.New("provider unavailable")

	// ErrAllProviders indicates all providers in the chain have been exhausted.
	ErrAllProviders = errors.New("all providers failed")

	// ErrNoProvider indicates no provider is configured for the requested role.
	ErrNoProvider = errors.New("no provider configured")
)

// IsRetryable reports whether the error is transient and the request
// can be retried with a different provider or after a delay.
func IsRetryable(err error) bool {
	return errors.Is(err, ErrRateLimit) || errors.Is(err, ErrProviderDown)
}
