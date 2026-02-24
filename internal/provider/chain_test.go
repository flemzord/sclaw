package provider_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/provider/providertest"
)

// syncBuffer is a thread-safe bytes.Buffer for concurrent log assertions.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

// testLogger returns a slog.Logger that writes to a thread-safe buffer for assertions.
func testLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), buf
}

func okProvider(name string) *providertest.MockProvider {
	return &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			return provider.CompletionResponse{Content: name}, nil
		},
		StreamFunc: func(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
			ch := make(chan provider.StreamChunk, 1)
			ch <- provider.StreamChunk{Content: name}
			close(ch)
			return ch, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return name },
		HealthCheckFunc:       func(_ context.Context) error { return nil },
	}
}

func failProvider(err error) *providertest.MockProvider {
	return &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			return provider.CompletionResponse{}, err
		},
		StreamFunc: func(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
			return nil, err
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "fail" },
		HealthCheckFunc:       func(_ context.Context) error { return err },
	}
}

func TestNewChain_Empty(t *testing.T) {
	t.Parallel()
	_, err := provider.NewChain(nil)
	if !errors.Is(err, provider.ErrNoProvider) {
		t.Fatalf("err = %v, want ErrNoProvider", err)
	}
}

func TestNewChain_NilProvider(t *testing.T) {
	t.Parallel()
	_, err := provider.NewChain([]provider.ChainEntry{
		{Name: "broken", Provider: nil, Role: provider.RolePrimary},
	})
	if !errors.Is(err, provider.ErrNoProvider) {
		t.Fatalf("err = %v, want ErrNoProvider", err)
	}
}

func TestProviderChain_SingleSuccess(t *testing.T) {
	t.Parallel()

	p := okProvider("p1")
	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p1", Provider: p, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "p1" {
		t.Errorf("content = %q, want %q", resp.Content, "p1")
	}
}

func TestProviderChain_Failover(t *testing.T) {
	t.Parallel()

	p1 := failProvider(provider.ErrProviderDown)
	p2 := okProvider("p2")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p1", Provider: p1, Role: provider.RolePrimary},
		{Name: "p2", Provider: p2, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "p2" {
		t.Errorf("content = %q, want %q", resp.Content, "p2")
	}
}

func TestProviderChain_AllFail(t *testing.T) {
	t.Parallel()

	p1 := failProvider(provider.ErrProviderDown)
	p2 := failProvider(provider.ErrRateLimit)

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p1", Provider: p1, Role: provider.RolePrimary},
		{Name: "p2", Provider: p2, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if !errors.Is(err, provider.ErrAllProviders) {
		t.Fatalf("err = %v, want ErrAllProviders", err)
	}
}

func TestProviderChain_NonRetryableStops(t *testing.T) {
	t.Parallel()

	p1 := failProvider(provider.ErrContextLength) // non-retryable
	p2 := okProvider("p2")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p1", Provider: p1, Role: provider.RolePrimary},
		{Name: "p2", Provider: p2, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if !errors.Is(err, provider.ErrContextLength) {
		t.Fatalf("err = %v, want ErrContextLength", err)
	}

	// p2 should NOT have been called.
	if p2.CompleteCalls > 0 {
		t.Error("p2 should not be called after non-retryable error")
	}
}

func TestProviderChain_NonRetryableDoesNotAffectHealth(t *testing.T) {
	t.Parallel()

	calls := 0
	p := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			calls++
			if calls == 1 {
				return provider.CompletionResponse{}, provider.ErrContextLength
			}
			return provider.CompletionResponse{Content: "ok"}, nil
		},
		StreamFunc: func(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
			return nil, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test" },
		HealthCheckFunc:       func(_ context.Context) error { return nil },
	}

	chain, err := provider.NewChain([]provider.ChainEntry{
		{
			Name:     "p",
			Provider: p,
			Role:     provider.RolePrimary,
			Health:   provider.HealthConfig{MaxFailures: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if !errors.Is(err, provider.ErrContextLength) {
		t.Fatalf("err = %v, want ErrContextLength", err)
	}

	resp, err := chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q, want %q", resp.Content, "ok")
	}
}

func TestProviderChain_ContextCanceledSkipsCompleteAttempt(t *testing.T) {
	t.Parallel()

	p := okProvider("p")
	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p", Provider: p, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = chain.Complete(ctx, provider.RolePrimary, provider.CompletionRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if p.CompleteCalls != 0 {
		t.Fatalf("CompleteCalls = %d, want 0", p.CompleteCalls)
	}
}

func TestProviderChain_RoleFiltering(t *testing.T) {
	t.Parallel()

	pPrimary := okProvider("primary")
	pInternal := okProvider("internal")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "pPrimary", Provider: pPrimary, Role: provider.RolePrimary},
		{Name: "pInternal", Provider: pInternal, Role: provider.RoleInternal},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := chain.Complete(context.Background(), provider.RoleInternal, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "internal" {
		t.Errorf("content = %q, want %q", resp.Content, "internal")
	}

	// Primary should not have been called.
	if pPrimary.CompleteCalls > 0 {
		t.Error("primary should not be called for internal role")
	}
}

func TestProviderChain_FallbackUnrestricted(t *testing.T) {
	t.Parallel()

	pPrimary := failProvider(provider.ErrProviderDown)
	pFallback := okProvider("fallback")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "primary", Provider: pPrimary, Role: provider.RolePrimary},
		{Name: "fallback", Provider: pFallback, Role: provider.RoleFallback},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "fallback" {
		t.Errorf("content = %q, want %q", resp.Content, "fallback")
	}
}

func TestProviderChain_FallbackRestricted(t *testing.T) {
	t.Parallel()

	pPrimary := failProvider(provider.ErrProviderDown)
	pInternal := failProvider(provider.ErrProviderDown)
	pFallback := okProvider("fallback")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "primary", Provider: pPrimary, Role: provider.RolePrimary},
		{Name: "internal", Provider: pInternal, Role: provider.RoleInternal},
		// Fallback only for primary, not internal.
		{Name: "fallback", Provider: pFallback, Role: provider.RoleFallback, FallbackFor: []provider.Role{provider.RolePrimary}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should work for primary.
	resp, err := chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("primary Complete: %v", err)
	}
	if resp.Content != "fallback" {
		t.Errorf("content = %q, want %q", resp.Content, "fallback")
	}

	// Should NOT work for internal.
	_, err = chain.Complete(context.Background(), provider.RoleInternal, provider.CompletionRequest{})
	if !errors.Is(err, provider.ErrAllProviders) {
		t.Fatalf("internal err = %v, want ErrAllProviders", err)
	}
}

func TestProviderChain_AuthRotation(t *testing.T) {
	t.Parallel()

	auth, err := provider.NewAuthProfile("key1", "key2", "key3")
	if err != nil {
		t.Fatal(err)
	}
	if auth.CurrentKey() != "key1" {
		t.Fatalf("initial key = %q, want %q", auth.CurrentKey(), "key1")
	}

	callCount := 0
	p := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			callCount++
			if callCount <= 2 {
				return provider.CompletionResponse{}, provider.ErrRateLimit
			}
			return provider.CompletionResponse{Content: "ok"}, nil
		},
		StreamFunc: func(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
			return nil, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test" },
		HealthCheckFunc:       func(_ context.Context) error { return nil },
	}

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p", Provider: p, Role: provider.RolePrimary, Auth: auth},
	})
	if err != nil {
		t.Fatal(err)
	}

	// First call: rate limited -> rotates to key2, records failure.
	_, _ = chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})

	if auth.CurrentKey() != "key2" {
		t.Errorf("key after first rate limit = %q, want %q", auth.CurrentKey(), "key2")
	}
}

func TestProviderChain_Stream(t *testing.T) {
	t.Parallel()

	p := okProvider("streamer")
	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p", Provider: p, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := chain.Stream(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	chunks := make([]provider.StreamChunk, 0, 1)
	for c := range ch {
		chunks = append(chunks, c)
	}

	if len(chunks) != 1 || chunks[0].Content != "streamer" {
		t.Errorf("chunks = %+v, want single chunk with content 'streamer'", chunks)
	}
}

func TestProviderChain_StreamFailover(t *testing.T) {
	t.Parallel()

	p1 := failProvider(provider.ErrProviderDown)
	p2 := okProvider("p2")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p1", Provider: p1, Role: provider.RolePrimary},
		{Name: "p2", Provider: p2, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := chain.Stream(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var content string
	for c := range ch {
		content += c.Content
	}
	if content != "p2" {
		t.Errorf("stream content = %q, want %q", content, "p2")
	}
}

func TestProviderChain_StreamNonRetryableStops(t *testing.T) {
	t.Parallel()

	p1 := failProvider(provider.ErrContextLength) // non-retryable
	p2 := okProvider("p2")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p1", Provider: p1, Role: provider.RolePrimary},
		{Name: "p2", Provider: p2, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = chain.Stream(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if !errors.Is(err, provider.ErrContextLength) {
		t.Fatalf("err = %v, want ErrContextLength", err)
	}
	if p2.StreamCalls > 0 {
		t.Error("p2 should not be called after non-retryable stream error")
	}
}

func TestProviderChain_StreamNonRetryableDoesNotAffectHealth(t *testing.T) {
	t.Parallel()

	calls := 0
	p := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			return provider.CompletionResponse{Content: "unused"}, nil
		},
		StreamFunc: func(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
			calls++
			if calls == 1 {
				return nil, provider.ErrContextLength
			}
			ch := make(chan provider.StreamChunk, 1)
			ch <- provider.StreamChunk{Content: "ok"}
			close(ch)
			return ch, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test" },
		HealthCheckFunc:       func(_ context.Context) error { return nil },
	}

	chain, err := provider.NewChain([]provider.ChainEntry{
		{
			Name:     "p",
			Provider: p,
			Role:     provider.RolePrimary,
			Health:   provider.HealthConfig{MaxFailures: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = chain.Stream(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if !errors.Is(err, provider.ErrContextLength) {
		t.Fatalf("err = %v, want ErrContextLength", err)
	}

	ch, err := chain.Stream(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("second Stream: %v", err)
	}
	var content string
	for c := range ch {
		content += c.Content
	}
	if content != "ok" {
		t.Fatalf("content = %q, want %q", content, "ok")
	}
}

func TestProviderChain_ContextCanceledSkipsStreamAttempt(t *testing.T) {
	t.Parallel()

	p := okProvider("p")
	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p", Provider: p, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = chain.Stream(ctx, provider.RolePrimary, provider.CompletionRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if p.StreamCalls != 0 {
		t.Fatalf("StreamCalls = %d, want 0", p.StreamCalls)
	}
}

func TestProviderChain_GetProvider(t *testing.T) {
	t.Parallel()

	pPrimary := okProvider("primary")
	pInternal := okProvider("internal")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "primary", Provider: pPrimary, Role: provider.RolePrimary},
		{Name: "internal", Provider: pInternal, Role: provider.RoleInternal},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := chain.GetProvider(provider.RoleInternal)
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelName() != "internal" {
		t.Errorf("ModelName() = %q, want %q", got.ModelName(), "internal")
	}
}

func TestProviderChain_GetProvider_NoMatch(t *testing.T) {
	t.Parallel()

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p", Provider: okProvider("p"), Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = chain.GetProvider(provider.RoleInternal)
	if !errors.Is(err, provider.ErrNoProvider) {
		t.Fatalf("err = %v, want ErrNoProvider", err)
	}
}

func TestProviderChain_NoProviderForRole(t *testing.T) {
	t.Parallel()

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p", Provider: okProvider("p"), Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = chain.Complete(context.Background(), provider.RoleInternal, provider.CompletionRequest{})
	if !errors.Is(err, provider.ErrNoProvider) {
		t.Fatalf("err = %v, want ErrNoProvider", err)
	}
}

func TestProviderChain_StartStop(t *testing.T) {
	t.Parallel()

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p", Provider: okProvider("p"), Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	chain.Start(ctx)
	chain.Start(ctx) // idempotent
	chain.Stop()
	chain.Stop() // idempotent
}

func TestProviderChain_HealthCheckRevival(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	healthy := false

	isHealthy := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return healthy
	}
	setHealthy := func(v bool) {
		mu.Lock()
		defer mu.Unlock()
		healthy = v
	}

	p := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			if isHealthy() {
				return provider.CompletionResponse{Content: "ok"}, nil
			}
			return provider.CompletionResponse{}, provider.ErrProviderDown
		},
		StreamFunc: func(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
			return nil, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "revivable" },
		HealthCheckFunc: func(_ context.Context) error {
			if isHealthy() {
				return nil
			}
			return fmt.Errorf("still down")
		},
	}

	chain, err := provider.NewChain([]provider.ChainEntry{
		{
			Name:     "p",
			Provider: p,
			Role:     provider.RolePrimary,
			Health:   provider.HealthConfig{CheckInterval: 50 * time.Millisecond, MaxFailures: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chain.Start(ctx)
	defer chain.Stop()

	// Drive provider to dead state.
	_, _ = chain.Complete(ctx, provider.RolePrimary, provider.CompletionRequest{})
	_, _ = chain.Complete(ctx, provider.RolePrimary, provider.CompletionRequest{})

	// Should be dead now.
	_, err = chain.Complete(ctx, provider.RolePrimary, provider.CompletionRequest{})
	if !errors.Is(err, provider.ErrAllProviders) {
		t.Fatalf("expected ErrAllProviders when dead, got %v", err)
	}

	// Revive provider.
	setHealthy(true)

	// Poll until the health check detects recovery.
	deadline := time.After(2 * time.Second)
	for {
		resp, err := chain.Complete(ctx, provider.RolePrimary, provider.CompletionRequest{})
		if err == nil {
			if resp.Content != "ok" {
				t.Errorf("content = %q, want %q", resp.Content, "ok")
			}
			break
		}

		select {
		case <-deadline:
			t.Fatalf("provider did not recover within deadline: %v", err)
		case <-time.After(25 * time.Millisecond):
			// retry
		}
	}
}

func TestProviderChain_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	p := okProvider("concurrent")
	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p", Provider: p, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	chain.Start(ctx)
	defer chain.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = chain.Complete(ctx, provider.RolePrimary, provider.CompletionRequest{})
		}()
		go func() {
			defer wg.Done()
			_, _ = chain.GetProvider(provider.RolePrimary)
		}()
	}
	wg.Wait()
}

func TestAuthProfile_Rotation(t *testing.T) {
	t.Parallel()

	auth, err := provider.NewAuthProfile("a", "b", "c")
	if err != nil {
		t.Fatal(err)
	}

	if got := auth.CurrentKey(); got != "a" {
		t.Fatalf("initial = %q, want %q", got, "a")
	}

	auth.Rotate()
	if got := auth.CurrentKey(); got != "b" {
		t.Fatalf("after 1st rotate = %q, want %q", got, "b")
	}

	auth.Rotate()
	if got := auth.CurrentKey(); got != "c" {
		t.Fatalf("after 2nd rotate = %q, want %q", got, "c")
	}

	auth.Rotate()
	if got := auth.CurrentKey(); got != "a" {
		t.Fatalf("after wrap = %q, want %q", got, "a")
	}
}

func TestAuthProfile_SingleKey(t *testing.T) {
	t.Parallel()

	auth, err := provider.NewAuthProfile("only")
	if err != nil {
		t.Fatal(err)
	}
	if rotated := auth.Rotate(); rotated {
		t.Error("single key should not rotate")
	}
	if got := auth.CurrentKey(); got != "only" {
		t.Fatalf("key = %q, want %q", got, "only")
	}
}

func TestAuthProfile_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	auth, err := provider.NewAuthProfile("a", "b", "c")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			auth.Rotate()
		}()
		go func() {
			defer wg.Done()
			_ = auth.CurrentKey()
		}()
	}
	wg.Wait()
}

func TestIsRateLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"direct", provider.ErrRateLimit, true},
		{"wrapped", fmt.Errorf("api: %w", provider.ErrRateLimit), true},
		{"other", provider.ErrProviderDown, false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := provider.IsRateLimit(tt.err); got != tt.want {
				t.Errorf("IsRateLimit(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- Observability / logging tests ---

func TestProviderChain_LogsFailover(t *testing.T) {
	t.Parallel()

	logger, buf := testLogger()

	p1 := failProvider(provider.ErrProviderDown)
	p2 := okProvider("p2")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "broken", Provider: p1, Role: provider.RolePrimary},
		{Name: "healthy", Provider: p2, Role: provider.RolePrimary},
	}, provider.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "p2" {
		t.Errorf("content = %q, want %q", resp.Content, "p2")
	}

	logs := buf.String()
	if !strings.Contains(logs, "provider failed, failing over") {
		t.Errorf("expected failover log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "provider=broken") {
		t.Errorf("expected provider name in log, got:\n%s", logs)
	}
}

func TestProviderChain_LogsAllExhausted(t *testing.T) {
	t.Parallel()

	logger, buf := testLogger()

	p1 := failProvider(provider.ErrProviderDown)
	p2 := failProvider(provider.ErrRateLimit)

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p1", Provider: p1, Role: provider.RolePrimary},
		{Name: "p2", Provider: p2, Role: provider.RolePrimary},
	}, provider.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	_, err = chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if !errors.Is(err, provider.ErrAllProviders) {
		t.Fatalf("err = %v, want ErrAllProviders", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "all providers exhausted") {
		t.Errorf("expected exhaustion log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "role=primary") {
		t.Errorf("expected role in log, got:\n%s", logs)
	}
}

func TestProviderChain_LogsAuthRotation(t *testing.T) {
	t.Parallel()

	logger, buf := testLogger()

	auth, err := provider.NewAuthProfile("key1", "key2")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	p := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			calls++
			if calls == 1 {
				return provider.CompletionResponse{}, provider.ErrRateLimit
			}
			return provider.CompletionResponse{Content: "ok"}, nil
		},
		StreamFunc: func(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
			return nil, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test" },
		HealthCheckFunc:       func(_ context.Context) error { return nil },
	}

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "rotator", Provider: p, Role: provider.RolePrimary, Auth: auth},
	}, provider.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	// First call rate-limits, triggers rotation; second call succeeds.
	_, _ = chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})

	logs := buf.String()
	if !strings.Contains(logs, "auth key rotated") {
		t.Errorf("expected auth rotation log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "provider=rotator") {
		t.Errorf("expected provider name in log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "key_index=") {
		t.Errorf("expected key_index in log, got:\n%s", logs)
	}
}

func TestProviderChain_LogsHealthStateTransitions(t *testing.T) {
	t.Parallel()

	logger, buf := testLogger()

	p := failProvider(provider.ErrProviderDown)

	chain, err := provider.NewChain([]provider.ChainEntry{
		{
			Name:     "fragile",
			Provider: p,
			Role:     provider.RolePrimary,
			Health:   provider.HealthConfig{MaxFailures: 2, InitialBackoff: time.Nanosecond},
		},
	}, provider.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	// First failure → cooldown.
	_, _ = chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})

	logs := buf.String()
	if !strings.Contains(logs, "provider entered cooldown") {
		t.Errorf("expected cooldown log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "provider=fragile") {
		t.Errorf("expected provider name in log, got:\n%s", logs)
	}

	// Let the nanosecond cooldown expire, then second failure → dead.
	time.Sleep(time.Millisecond)
	buf.Reset()
	_, _ = chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})

	logs = buf.String()
	if !strings.Contains(logs, "provider marked dead") {
		t.Errorf("expected dead log, got:\n%s", logs)
	}
}

func TestProviderChain_LogsRevival(t *testing.T) {
	t.Parallel()

	logger, buf := testLogger()

	var mu sync.Mutex
	healthy := false

	isHealthy := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return healthy
	}
	setHealthy := func(v bool) {
		mu.Lock()
		defer mu.Unlock()
		healthy = v
	}

	p := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			if isHealthy() {
				return provider.CompletionResponse{Content: "ok"}, nil
			}
			return provider.CompletionResponse{}, provider.ErrProviderDown
		},
		StreamFunc: func(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
			return nil, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "revivable" },
		HealthCheckFunc: func(_ context.Context) error {
			if isHealthy() {
				return nil
			}
			return fmt.Errorf("still down")
		},
	}

	chain, err := provider.NewChain([]provider.ChainEntry{
		{
			Name:     "revivable",
			Provider: p,
			Role:     provider.RolePrimary,
			Health:   provider.HealthConfig{CheckInterval: 50 * time.Millisecond, MaxFailures: 2},
		},
	}, provider.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chain.Start(ctx)
	defer chain.Stop()

	// Drive provider to dead state.
	_, _ = chain.Complete(ctx, provider.RolePrimary, provider.CompletionRequest{})
	_, _ = chain.Complete(ctx, provider.RolePrimary, provider.CompletionRequest{})

	// Revive provider.
	setHealthy(true)

	// Wait for health check to revive.
	deadline := time.After(2 * time.Second)
	for {
		resp, err := chain.Complete(ctx, provider.RolePrimary, provider.CompletionRequest{})
		if err == nil {
			if resp.Content != "ok" {
				t.Errorf("content = %q, want %q", resp.Content, "ok")
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("provider did not recover within deadline: %v", err)
		case <-time.After(25 * time.Millisecond):
		}
	}

	logs := buf.String()
	if !strings.Contains(logs, "provider revived") {
		t.Errorf("expected revival log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "provider=revivable") {
		t.Errorf("expected provider name in log, got:\n%s", logs)
	}
}

func TestProviderChain_StreamLogsFailover(t *testing.T) {
	t.Parallel()

	logger, buf := testLogger()

	p1 := failProvider(provider.ErrProviderDown)
	p2 := okProvider("p2")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "broken", Provider: p1, Role: provider.RolePrimary},
		{Name: "healthy", Provider: p2, Role: provider.RolePrimary},
	}, provider.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	ch, err := chain.Stream(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Drain channel.
	for range ch { //nolint:revive // intentional drain
	}

	logs := buf.String()
	if !strings.Contains(logs, "provider failed, failing over") {
		t.Errorf("expected failover log for stream, got:\n%s", logs)
	}
	if !strings.Contains(logs, "provider=broken") {
		t.Errorf("expected provider name in log, got:\n%s", logs)
	}
}

func TestProviderChain_NoLogWithoutLogger(t *testing.T) {
	t.Parallel()

	// Chain without WithLogger — should not panic.
	p1 := failProvider(provider.ErrProviderDown)
	p2 := okProvider("p2")

	chain, err := provider.NewChain([]provider.ChainEntry{
		{Name: "p1", Provider: p1, Role: provider.RolePrimary},
		{Name: "p2", Provider: p2, Role: provider.RolePrimary},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "p2" {
		t.Errorf("content = %q, want %q", resp.Content, "p2")
	}
}

func TestNewAuthProfile_EmptyKeys(t *testing.T) {
	t.Parallel()

	_, err := provider.NewAuthProfile()
	if !errors.Is(err, provider.ErrNoKeys) {
		t.Fatalf("err = %v, want ErrNoKeys", err)
	}
}

func TestProviderChain_StreamMidStreamError(t *testing.T) {
	t.Parallel()

	logger, buf := testLogger()

	p := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			return provider.CompletionResponse{}, nil
		},
		StreamFunc: func(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
			ch := make(chan provider.StreamChunk, 3)
			ch <- provider.StreamChunk{Content: "hello "}
			ch <- provider.StreamChunk{Err: provider.ErrProviderDown} // mid-stream retryable error
			ch <- provider.StreamChunk{Content: "world"}
			close(ch)
			return ch, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "flaky" },
		HealthCheckFunc:       func(_ context.Context) error { return nil },
	}

	chain, err := provider.NewChain([]provider.ChainEntry{
		{
			Name:     "flaky",
			Provider: p,
			Role:     provider.RolePrimary,
			Health:   provider.HealthConfig{MaxFailures: 5},
		},
	}, provider.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	ch, err := chain.Stream(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Consume all chunks.
	chunks := make([]provider.StreamChunk, 0, 3)
	for c := range ch {
		chunks = append(chunks, c)
	}

	// All 3 chunks should be forwarded.
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3", len(chunks))
	}
	if chunks[0].Content != "hello " {
		t.Errorf("chunk[0] = %q, want %q", chunks[0].Content, "hello ")
	}
	if chunks[1].Err == nil {
		t.Error("chunk[1] should have an error")
	}

	logs := buf.String()
	if !strings.Contains(logs, "mid-stream error degraded provider health") {
		t.Errorf("expected mid-stream error log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "provider=flaky") {
		t.Errorf("expected provider name in log, got:\n%s", logs)
	}
}

func TestProviderChain_StreamSuccessAfterFullConsumption(t *testing.T) {
	t.Parallel()

	p := okProvider("streamer")
	chain, err := provider.NewChain([]provider.ChainEntry{
		{
			Name:     "streamer",
			Provider: p,
			Role:     provider.RolePrimary,
			Health:   provider.HealthConfig{MaxFailures: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := chain.Stream(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Consume all chunks — success should be recorded after full drain.
	for range ch { //nolint:revive // intentional drain
	}

	// Verify provider is still healthy by making another call.
	resp, err := chain.Complete(context.Background(), provider.RolePrimary, provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete after stream: %v", err)
	}
	if resp.Content != "streamer" {
		t.Errorf("content = %q, want %q", resp.Content, "streamer")
	}
}
