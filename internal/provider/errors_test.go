package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		ErrRateLimit,
		ErrContextLength,
		ErrProviderDown,
		ErrAllProviders,
		ErrNoProvider,
	}

	for _, err := range sentinels {
		if err == nil {
			t.Fatal("sentinel error must not be nil")
		}
		if err.Error() == "" {
			t.Fatalf("sentinel error %v must have a non-empty message", err)
		}
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		ErrRateLimit,
		ErrContextLength,
		ErrProviderDown,
		ErrAllProviders,
		ErrNoProvider,
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatalf("sentinel errors must be distinct: %v and %v", a, b)
			}
		}
	}
}

func TestIsRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"rate limit", ErrRateLimit, true},
		{"provider down", ErrProviderDown, true},
		{"context length", ErrContextLength, false},
		{"all providers", ErrAllProviders, false},
		{"no provider", ErrNoProvider, false},
		{"generic error", errors.New("something"), false},
		{"wrapped rate limit", fmt.Errorf("api: %w", ErrRateLimit), true},
		{"wrapped provider down", fmt.Errorf("api: %w", ErrProviderDown), true},
		{"wrapped context length", fmt.Errorf("api: %w", ErrContextLength), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
