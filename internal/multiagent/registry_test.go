package multiagent

import (
	"errors"
	"testing"

	"github.com/flemzord/sclaw/pkg/message"
)

func TestRegistry_ResolveByUser(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"support": {
			Routing: RoutingConfig{Users: []string{"user1", "user2"}},
		},
		"sales": {
			Routing: RoutingConfig{Users: []string{"user3"}},
		},
	}
	order := []string{"support", "sales"}

	reg, err := NewRegistry(agents, order)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	msg := message.InboundMessage{
		Channel: "telegram",
		Sender:  message.Sender{ID: "user1"},
		Chat:    message.Chat{ID: "somegroup"},
	}

	got, err := reg.Resolve(msg)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "support" {
		t.Errorf("Resolve() = %q, want %q", got, "support")
	}
}

func TestRegistry_ResolveByGroup(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"hr": {
			Routing: RoutingConfig{Groups: []string{"group-hr"}},
		},
	}
	order := []string{"hr"}

	reg, err := NewRegistry(agents, order)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	msg := message.InboundMessage{
		Channel: "slack",
		Sender:  message.Sender{ID: "unknown-user"},
		Chat:    message.Chat{ID: "group-hr"},
	}

	got, err := reg.Resolve(msg)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "hr" {
		t.Errorf("Resolve() = %q, want %q", got, "hr")
	}
}

func TestRegistry_ResolveByChannel(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"telegram-bot": {
			Routing: RoutingConfig{Channels: []string{"telegram"}},
		},
		"slack-bot": {
			Routing: RoutingConfig{Channels: []string{"slack"}},
		},
	}
	order := []string{"slack-bot", "telegram-bot"}

	reg, err := NewRegistry(agents, order)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	msg := message.InboundMessage{
		Channel: "telegram",
		Sender:  message.Sender{ID: "someone"},
		Chat:    message.Chat{ID: "chat1"},
	}

	got, err := reg.Resolve(msg)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "telegram-bot" {
		t.Errorf("Resolve() = %q, want %q", got, "telegram-bot")
	}
}

func TestRegistry_ResolveDefault(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"specific": {
			Routing: RoutingConfig{Users: []string{"vip"}},
		},
		"fallback": {
			Routing: RoutingConfig{Default: true},
		},
	}
	order := []string{"fallback", "specific"}

	reg, err := NewRegistry(agents, order)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	msg := message.InboundMessage{
		Channel: "discord",
		Sender:  message.Sender{ID: "random-user"},
		Chat:    message.Chat{ID: "random-chat"},
	}

	got, err := reg.Resolve(msg)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "fallback" {
		t.Errorf("Resolve() = %q, want %q", got, "fallback")
	}
}

func TestRegistry_ResolveNoMatch(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"niche": {
			Routing: RoutingConfig{Users: []string{"special-user"}},
		},
	}
	order := []string{"niche"}

	reg, err := NewRegistry(agents, order)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	msg := message.InboundMessage{
		Channel: "telegram",
		Sender:  message.Sender{ID: "nobody"},
		Chat:    message.Chat{ID: "nowhere"},
	}

	_, err = reg.Resolve(msg)
	if !errors.Is(err, ErrNoMatchingAgent) {
		t.Errorf("Resolve() error = %v, want %v", err, ErrNoMatchingAgent)
	}
}

func TestRegistry_DuplicateDefault(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"a": {Routing: RoutingConfig{Default: true}},
		"b": {Routing: RoutingConfig{Default: true}},
	}
	order := []string{"a", "b"}

	_, err := NewRegistry(agents, order)
	if !errors.Is(err, ErrDuplicateDefault) {
		t.Errorf("NewRegistry() error = %v, want %v", err, ErrDuplicateDefault)
	}
}

func TestRegistry_PriorityCascade(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"user-agent": {
			Routing: RoutingConfig{Users: []string{"user1"}},
		},
		"channel-agent": {
			Routing: RoutingConfig{Channels: []string{"telegram"}},
		},
		"default-agent": {
			Routing: RoutingConfig{Default: true},
		},
	}
	order := []string{"channel-agent", "default-agent", "user-agent"}

	reg, err := NewRegistry(agents, order)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	// User match should win over channel match and default.
	msg := message.InboundMessage{
		Channel: "telegram",
		Sender:  message.Sender{ID: "user1"},
		Chat:    message.Chat{ID: "some-chat"},
	}

	got, err := reg.Resolve(msg)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "user-agent" {
		t.Errorf("Resolve() = %q, want %q (user should win over channel)", got, "user-agent")
	}
}

func TestRegistry_AgentConfig(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"bot": {
			Workspace: "ws1",
			Provider:  "openai",
			Tools:     []string{"search", "calc"},
		},
	}
	order := []string{"bot"}

	reg, err := NewRegistry(agents, order)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	cfg, ok := reg.AgentConfig("bot")
	if !ok {
		t.Fatal("AgentConfig(\"bot\") not found")
	}
	if cfg.Provider != "openai" {
		t.Errorf("AgentConfig().Provider = %q, want %q", cfg.Provider, "openai")
	}
	if len(cfg.Tools) != 2 {
		t.Errorf("AgentConfig().Tools length = %d, want 2", len(cfg.Tools))
	}

	_, ok = reg.AgentConfig("nonexistent")
	if ok {
		t.Error("AgentConfig(\"nonexistent\") should return false")
	}
}

func TestRegistry_AgentIDs(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"alpha":   {},
		"beta":    {},
		"charlie": {},
	}
	order := []string{"alpha", "beta", "charlie"}

	reg, err := NewRegistry(agents, order)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	ids := reg.AgentIDs()
	if len(ids) != 3 {
		t.Fatalf("AgentIDs() length = %d, want 3", len(ids))
	}
	want := []string{"alpha", "beta", "charlie"}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("AgentIDs()[%d] = %q, want %q", i, id, want[i])
		}
	}
}
