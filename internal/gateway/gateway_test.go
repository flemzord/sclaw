package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/router"
	"gopkg.in/yaml.v3"
)

func TestGateway_ModuleInfo(t *testing.T) {
	t.Parallel()

	g := &Gateway{}
	info := g.ModuleInfo()

	if info.ID != "gateway.http" {
		t.Errorf("ID = %q, want %q", info.ID, "gateway.http")
	}
	if info.New == nil {
		t.Fatal("New func is nil")
	}

	mod := info.New()
	if _, ok := mod.(*Gateway); !ok {
		t.Error("New() should return *Gateway")
	}
}

func TestGateway_ConfigureDefaults(t *testing.T) {
	t.Parallel()

	g := &Gateway{}

	node := mustYAMLNode(t, "{}")
	if err := g.Configure(node); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	if g.config.Bind != "127.0.0.1:8080" {
		t.Errorf("Bind = %q, want default", g.config.Bind)
	}
	if g.config.ReadTimeout != 10*time.Second {
		t.Errorf("ReadTimeout = %v, want 10s", g.config.ReadTimeout)
	}
	if g.config.WriteTimeout != 30*time.Second {
		t.Errorf("WriteTimeout = %v, want 30s", g.config.WriteTimeout)
	}
	if g.config.ShutdownTimeout != 5*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 5s", g.config.ShutdownTimeout)
	}
}

func TestGateway_ConfigureCustom(t *testing.T) {
	t.Parallel()

	g := &Gateway{}
	node := mustYAMLNode(t, `
bind: "0.0.0.0:9090"
read_timeout: 5s
write_timeout: 15s
shutdown_timeout: 10s
auth:
  bearer_token: "my-token"
webhooks:
  github:
    secret: "gh-secret"
`)

	if err := g.Configure(node); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	if g.config.Bind != "0.0.0.0:9090" {
		t.Errorf("Bind = %q, want custom", g.config.Bind)
	}
	if g.config.Auth.BearerToken != "my-token" {
		t.Errorf("BearerToken = %q", g.config.Auth.BearerToken)
	}
	if wh, ok := g.config.Webhooks["github"]; !ok || wh.Secret != "gh-secret" {
		t.Errorf("Webhooks = %+v", g.config.Webhooks)
	}
}

func TestGateway_Provision(t *testing.T) {
	t.Parallel()

	g := &Gateway{}
	g.config.defaults()

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	appCtx := core.NewAppContext(logger, "/data", "/ws")

	if err := g.Provision(appCtx); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if g.metrics == nil {
		t.Error("metrics should be initialized")
	}
	if g.dispatcher == nil {
		t.Error("dispatcher should be initialized")
	}

	if _, ok := appCtx.Service("gateway.metrics"); !ok {
		t.Error("gateway.metrics not registered")
	}
	if _, ok := appCtx.Service("gateway.webhook_dispatcher"); !ok {
		t.Error("gateway.webhook_dispatcher not registered")
	}
}

func TestGateway_ValidateGoodAddress(t *testing.T) {
	t.Parallel()

	g := &Gateway{}
	g.config.Bind = "127.0.0.1:8080"
	if err := g.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestGateway_ValidateBadAddress(t *testing.T) {
	t.Parallel()

	g := &Gateway{}
	g.config.Bind = "not a valid address::"
	if err := g.Validate(); err == nil {
		t.Error("expected validation error for bad address")
	}
}

// freeAddr returns a free TCP address on localhost.
func freeAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// doGet makes a GET request with context.
func doGet(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// doGetWithBearer makes a GET request with a bearer token.
func doGetWithBearer(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func newTestGateway(t *testing.T, addr string, auth AuthConfig) *Gateway {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	appCtx := core.NewAppContext(logger, "/data", "/ws")

	g := &Gateway{}
	g.config = Config{
		Bind:            addr,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		Auth:            auth,
	}
	g.appCtx = appCtx
	g.logger = logger
	g.metrics = &Metrics{}
	g.dispatcher = NewWebhookDispatcher(logger)
	return g
}

func TestGateway_StartStop(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	g := newTestGateway(t, addr, AuthConfig{})

	if err := g.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	resp := doGet(t, "http://"+addr+"/health")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("health.Status = %q, want %q", health.Status, "ok")
	}

	if err := g.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestGateway_StartWithServices(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	appCtx := core.NewAppContext(logger, "/data", "/ws")

	store := router.NewInMemorySessionStore()
	store.GetOrCreate(router.SessionKey{Channel: "test", ChatID: "1"})
	appCtx.RegisterService("router.sessions", store)

	chain, _ := provider.NewChain([]provider.ChainEntry{
		{Name: "p1", Provider: &fakeProvider{name: "p1"}, Role: provider.RolePrimary},
	})
	appCtx.RegisterService("provider.chain", chain)

	g := &Gateway{}
	g.config = Config{
		Bind:            addr,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		ShutdownTimeout: 2 * time.Second,
	}
	g.appCtx = appCtx
	g.logger = logger
	g.metrics = &Metrics{}
	g.dispatcher = NewWebhookDispatcher(logger)

	if err := g.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = g.Stop(context.Background()) }()

	resp := doGet(t, "http://"+addr+"/health")
	defer func() { _ = resp.Body.Close() }()

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if health.Sessions != 1 {
		t.Errorf("sessions = %d, want 1", health.Sessions)
	}
	if len(health.Providers) != 1 {
		t.Errorf("providers = %d, want 1", len(health.Providers))
	}
}

func TestGateway_AdminNotMountedWithoutAuth(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	g := newTestGateway(t, addr, AuthConfig{})

	if err := g.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = g.Stop(context.Background()) }()

	// /status should return 404 without auth configured.
	resp := doGet(t, "http://"+addr+"/status")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want 404 or 405 (not mounted)", resp.StatusCode)
	}

	// /api/sessions should also not be accessible.
	resp2 := doGet(t, "http://"+addr+"/api/sessions")
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound && resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("sessions code = %d, want 404 or 405 (not mounted)", resp2.StatusCode)
	}
}

func TestGateway_AdminWithAuth(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	g := newTestGateway(t, addr, AuthConfig{BearerToken: "test-token"})
	g.sessions = router.NewInMemorySessionStore()

	if err := g.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = g.Stop(context.Background()) }()

	// Without token → 401.
	resp := doGet(t, "http://"+addr+"/status")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no-auth status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	// With valid token → 200.
	resp2 := doGetWithBearer(t, "http://"+addr+"/status", "test-token")
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("auth status = %d, want %d", resp2.StatusCode, http.StatusOK)
	}
}

func TestGateway_StopNilServer(t *testing.T) {
	t.Parallel()

	g := &Gateway{}
	if err := g.Stop(context.Background()); err != nil {
		t.Errorf("Stop on nil server should not error: %v", err)
	}
}

// mustYAMLNode parses YAML text into a *yaml.Node for Configure calls.
func mustYAMLNode(t *testing.T, text string) *yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(text), &node); err != nil {
		t.Fatalf("YAML parse: %v", err)
	}
	if len(node.Content) > 0 {
		return node.Content[0]
	}
	return &node
}
