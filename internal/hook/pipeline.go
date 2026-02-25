package hook

import (
	"context"
	"slices"
	"sync"
)

// Pipeline manages hook registration and execution.
// Hooks are grouped by position and sorted by (priority, registration order).
// Thread-safe: registrations use a write lock, executions use a read lock.
type Pipeline struct {
	mu    sync.RWMutex
	hooks map[Position][]Hook
	// order tracks registration sequence for stable sorting.
	order map[Hook]int
	seq   int
}

// NewPipeline creates a new empty hook pipeline.
func NewPipeline() *Pipeline {
	return &Pipeline{
		hooks: make(map[Position][]Hook),
		order: make(map[Hook]int),
	}
}

// Register adds a hook to the pipeline. Hooks within the same position
// are sorted by priority (ascending), with registration order as tiebreaker.
func (p *Pipeline) Register(h Hook) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pos := h.Position()
	p.order[h] = p.seq
	p.seq++

	p.hooks[pos] = append(p.hooks[pos], h)
	slices.SortStableFunc(p.hooks[pos], func(a, b Hook) int {
		if a.Priority() != b.Priority() {
			return a.Priority() - b.Priority()
		}
		return p.order[a] - p.order[b]
	})
}

// RunBeforeProcess executes all BeforeProcess hooks in order.
// Short-circuits on ActionDrop. Errors are logged but don't stop execution.
func (p *Pipeline) RunBeforeProcess(ctx context.Context, hctx *Context) (Action, error) {
	p.mu.RLock()
	hooks := p.hooks[BeforeProcess]
	p.mu.RUnlock()

	for _, h := range hooks {
		action, err := h.Execute(ctx, hctx)
		if err != nil && hctx.Logger != nil {
			hctx.Logger.Warn("hook: before_process error",
				"error", err,
				"priority", h.Priority(),
			)
		}
		if action == ActionDrop {
			return ActionDrop, nil
		}
	}
	return ActionContinue, nil
}

// RunBeforeSend executes all BeforeSend hooks in order.
// Returns ActionModify if any hook signaled modification. Errors are logged.
func (p *Pipeline) RunBeforeSend(ctx context.Context, hctx *Context) (Action, error) {
	p.mu.RLock()
	hooks := p.hooks[BeforeSend]
	p.mu.RUnlock()

	modified := false
	for _, h := range hooks {
		action, err := h.Execute(ctx, hctx)
		if err != nil && hctx.Logger != nil {
			hctx.Logger.Warn("hook: before_send error",
				"error", err,
				"priority", h.Priority(),
			)
		}
		if action == ActionModify {
			modified = true
		}
	}
	if modified {
		return ActionModify, nil
	}
	return ActionContinue, nil
}

// RunAfterSend executes all AfterSend hooks. Fire-and-forget: errors
// are logged internally and never propagated to the caller.
func (p *Pipeline) RunAfterSend(ctx context.Context, hctx *Context) {
	p.mu.RLock()
	hooks := p.hooks[AfterSend]
	p.mu.RUnlock()

	for _, h := range hooks {
		if _, err := h.Execute(ctx, hctx); err != nil && hctx.Logger != nil {
			hctx.Logger.Warn("hook: after_send error",
				"error", err,
				"priority", h.Priority(),
			)
		}
	}
}
