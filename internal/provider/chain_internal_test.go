package provider

import (
	"testing"
	"time"
)

func TestMinHealthCheckInterval(t *testing.T) {
	t.Parallel()

	got := minHealthCheckInterval([]chainEntry{
		{ChainEntry: ChainEntry{Health: HealthConfig{CheckInterval: 30 * time.Second}}},
		{ChainEntry: ChainEntry{Health: HealthConfig{CheckInterval: 20 * time.Second}}},
		{ChainEntry: ChainEntry{Health: HealthConfig{CheckInterval: 25 * time.Second}}},
	})

	if got != 20*time.Second {
		t.Fatalf("interval = %v, want %v", got, 20*time.Second)
	}
}

func TestMinHealthCheckInterval_DefaultsAndEmpty(t *testing.T) {
	t.Parallel()

	if got := minHealthCheckInterval(nil); got != 10*time.Second {
		t.Fatalf("empty interval = %v, want %v", got, 10*time.Second)
	}

	got := minHealthCheckInterval([]chainEntry{
		{ChainEntry: ChainEntry{Health: HealthConfig{CheckInterval: -1 * time.Second}}},
		{ChainEntry: ChainEntry{Health: HealthConfig{CheckInterval: 0}}},
		{ChainEntry: ChainEntry{Health: HealthConfig{CheckInterval: 15 * time.Second}}},
	})

	if got != 10*time.Second {
		t.Fatalf("interval = %v, want %v", got, 10*time.Second)
	}
}
