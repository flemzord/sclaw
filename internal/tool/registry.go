package tool

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// Schema is a tool's name paired with its JSON Schema, returned by Registry.Schemas.
type Schema struct {
	Name   string
	Schema json.RawMessage
}

// Registry holds registered tools and orchestrates their execution
// through the policy and approval system.
// It is instance-based (not global) for better testability.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
// It returns ErrNoScopes if the tool declares no scopes,
// and ErrDuplicateTool if a tool with the same name is already registered.
func (r *Registry) Register(t Tool) error {
	name := strings.TrimSpace(t.Name())
	if name == "" {
		return ErrEmptyToolName
	}
	if len(t.Scopes()) == 0 {
		return fmt.Errorf("%w: %s", ErrNoScopes, name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateTool, name)
	}

	r.tools[name] = t
	return nil
}

// Get returns the tool with the given name, or ErrToolNotFound.
func (r *Registry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return t, nil
}

// Schemas returns all registered tool schemas sorted by name.
func (r *Registry) Schemas() []Schema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemas := make([]Schema, 0, len(r.tools))
	for name, t := range r.tools {
		schemas = append(schemas, Schema{
			Name:   name,
			Schema: t.Schema(),
		})
	}
	slices.SortFunc(schemas, func(a, b Schema) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return schemas
}

// Names returns all registered tool names sorted alphabetically.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Execute orchestrates tool execution: lookup → policy resolution → elevated
// adjustment → deny/allow/ask flow.
func (r *Registry) Execute(
	ctx context.Context,
	name string,
	args json.RawMessage,
	policyCfg PolicyConfig,
	policyCtx PolicyContext,
	elevated *ElevatedState,
	requester ApprovalRequester,
	timeout time.Duration,
	env ExecutionEnv,
) (Output, error) {
	// Lookup the tool.
	t, err := r.Get(name)
	if err != nil {
		return Output{}, err
	}

	// Resolve the effective policy.
	level := ResolvePolicy(policyCfg, policyCtx, t)

	// Apply elevated state if provided.
	if elevated != nil {
		level = elevated.Apply(level)
	}

	switch level {
	case ApprovalDeny:
		return Output{}, fmt.Errorf("%w: %s", ErrDenied, name)

	case ApprovalAllow:
		return t.Execute(ctx, args, env)

	case ApprovalAsk:
		if requester == nil {
			return Output{}, fmt.Errorf("%w: %s (no approval requester)", ErrDenied, name)
		}

		pending := NewPendingApproval()
		resp, err := pending.Begin(ctx, requester, ApprovalRequest{
			ID:          fmt.Sprintf("approve-%s-%d", name, time.Now().UnixNano()),
			ToolName:    name,
			Description: t.Description(),
			Arguments:   args,
			Context:     policyCtx,
		}, timeout)
		if err != nil {
			return Output{}, err
		}

		if !resp.Approved {
			return Output{}, fmt.Errorf("%w: %s (user denied: %s)", ErrDenied, name, resp.Reason)
		}

		return t.Execute(ctx, args, env)

	default:
		return Output{}, fmt.Errorf("%w: %s (unknown policy level: %s)", ErrDenied, name, level)
	}
}
