package hook

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/flemzord/sclaw/pkg/message"
)

// testHook is a minimal Hook for unit tests.
type testHook struct {
	pos         Position
	priority    int
	executeFunc func(ctx context.Context, hctx *Context) (Action, error)
}

func (h *testHook) Position() Position { return h.pos }
func (h *testHook) Priority() int      { return h.priority }
func (h *testHook) Execute(ctx context.Context, hctx *Context) (Action, error) {
	if h.executeFunc != nil {
		return h.executeFunc(ctx, hctx)
	}
	return ActionContinue, nil
}

func testContext() *Context {
	return &Context{
		Inbound:  message.InboundMessage{},
		Metadata: make(map[string]any),
		Logger:   slog.Default(),
	}
}

func TestPipeline_Register_SortsByPriority(t *testing.T) {
	t.Parallel()

	p := NewPipeline()

	var order []int
	makeHook := func(prio int) *testHook {
		return &testHook{
			pos:      BeforeProcess,
			priority: prio,
			executeFunc: func(_ context.Context, _ *Context) (Action, error) {
				order = append(order, prio)
				return ActionContinue, nil
			},
		}
	}

	p.Register(makeHook(10))
	p.Register(makeHook(1))
	p.Register(makeHook(5))

	hctx := testContext()
	hctx.Position = BeforeProcess
	_, _ = p.RunBeforeProcess(context.Background(), hctx)

	if len(order) != 3 {
		t.Fatalf("expected 3 hooks to run, got %d", len(order))
	}
	if order[0] != 1 || order[1] != 5 || order[2] != 10 {
		t.Errorf("execution order = %v, want [1 5 10]", order)
	}
}

func TestPipeline_Register_StableOrderForSamePriority(t *testing.T) {
	t.Parallel()

	p := NewPipeline()

	var order []string
	makeHook := func(name string) *testHook {
		return &testHook{
			pos:      BeforeProcess,
			priority: 0,
			executeFunc: func(_ context.Context, _ *Context) (Action, error) {
				order = append(order, name)
				return ActionContinue, nil
			},
		}
	}

	p.Register(makeHook("first"))
	p.Register(makeHook("second"))
	p.Register(makeHook("third"))

	hctx := testContext()
	hctx.Position = BeforeProcess
	_, _ = p.RunBeforeProcess(context.Background(), hctx)

	if len(order) != 3 {
		t.Fatalf("expected 3 hooks to run, got %d", len(order))
	}
	if order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Errorf("execution order = %v, want [first second third]", order)
	}
}

func TestPipeline_RunBeforeProcess_DropShortCircuits(t *testing.T) {
	t.Parallel()

	p := NewPipeline()

	reached := false
	p.Register(&testHook{
		pos:      BeforeProcess,
		priority: 1,
		executeFunc: func(_ context.Context, _ *Context) (Action, error) {
			return ActionDrop, nil
		},
	})
	p.Register(&testHook{
		pos:      BeforeProcess,
		priority: 2,
		executeFunc: func(_ context.Context, _ *Context) (Action, error) {
			reached = true
			return ActionContinue, nil
		},
	})

	hctx := testContext()
	action, _ := p.RunBeforeProcess(context.Background(), hctx)

	if action != ActionDrop {
		t.Errorf("action = %d, want ActionDrop", action)
	}
	if reached {
		t.Error("second hook should not have been reached after drop")
	}
}

func TestPipeline_RunBeforeProcess_ErrorDoesNotStopExecution(t *testing.T) {
	t.Parallel()

	p := NewPipeline()

	var secondCalled bool
	p.Register(&testHook{
		pos:      BeforeProcess,
		priority: 1,
		executeFunc: func(_ context.Context, _ *Context) (Action, error) {
			return ActionContinue, errors.New("hook error")
		},
	})
	p.Register(&testHook{
		pos:      BeforeProcess,
		priority: 2,
		executeFunc: func(_ context.Context, _ *Context) (Action, error) {
			secondCalled = true
			return ActionContinue, nil
		},
	})

	hctx := testContext()
	action, _ := p.RunBeforeProcess(context.Background(), hctx)

	if action != ActionContinue {
		t.Errorf("action = %d, want ActionContinue", action)
	}
	if !secondCalled {
		t.Error("second hook should have been called despite first hook's error")
	}
}

func TestPipeline_RunBeforeSend_ReturnsModifyIfAny(t *testing.T) {
	t.Parallel()

	p := NewPipeline()

	p.Register(&testHook{
		pos:      BeforeSend,
		priority: 1,
		executeFunc: func(_ context.Context, _ *Context) (Action, error) {
			return ActionContinue, nil
		},
	})
	p.Register(&testHook{
		pos:      BeforeSend,
		priority: 2,
		executeFunc: func(_ context.Context, _ *Context) (Action, error) {
			return ActionModify, nil
		},
	})

	hctx := testContext()
	action, _ := p.RunBeforeSend(context.Background(), hctx)

	if action != ActionModify {
		t.Errorf("action = %d, want ActionModify", action)
	}
}

func TestPipeline_RunAfterSend_ErrorsLogged(t *testing.T) {
	t.Parallel()

	p := NewPipeline()

	called := false
	p.Register(&testHook{
		pos:      AfterSend,
		priority: 1,
		executeFunc: func(_ context.Context, _ *Context) (Action, error) {
			return ActionContinue, errors.New("after send error")
		},
	})
	p.Register(&testHook{
		pos:      AfterSend,
		priority: 2,
		executeFunc: func(_ context.Context, _ *Context) (Action, error) {
			called = true
			return ActionContinue, nil
		},
	})

	hctx := testContext()
	p.RunAfterSend(context.Background(), hctx)

	if !called {
		t.Error("second after_send hook should have been called despite first hook's error")
	}
}

func TestPipeline_NoHooksRegistered(t *testing.T) {
	t.Parallel()

	p := NewPipeline()
	hctx := testContext()

	action, err := p.RunBeforeProcess(context.Background(), hctx)
	if action != ActionContinue || err != nil {
		t.Errorf("RunBeforeProcess = (%d, %v), want (ActionContinue, nil)", action, err)
	}

	action, err = p.RunBeforeSend(context.Background(), hctx)
	if action != ActionContinue || err != nil {
		t.Errorf("RunBeforeSend = (%d, %v), want (ActionContinue, nil)", action, err)
	}

	// RunAfterSend returns nothing — just verify no panic.
	p.RunAfterSend(context.Background(), hctx)
}

func TestPipeline_MetadataSharing(t *testing.T) {
	t.Parallel()

	p := NewPipeline()

	p.Register(&testHook{
		pos:      BeforeProcess,
		priority: 1,
		executeFunc: func(_ context.Context, hctx *Context) (Action, error) {
			hctx.Metadata["key"] = "value"
			return ActionContinue, nil
		},
	})

	hctx := testContext()
	_, _ = p.RunBeforeProcess(context.Background(), hctx)

	if v, ok := hctx.Metadata["key"]; !ok || v != "value" {
		t.Errorf("expected metadata key=value, got %v", hctx.Metadata)
	}
}

func TestPipeline_PositionIsolation(t *testing.T) {
	t.Parallel()

	p := NewPipeline()

	var beforeProcessCalled, beforeSendCalled bool
	p.Register(&testHook{
		pos: BeforeProcess,
		executeFunc: func(_ context.Context, _ *Context) (Action, error) {
			beforeProcessCalled = true
			return ActionContinue, nil
		},
	})
	p.Register(&testHook{
		pos: BeforeSend,
		executeFunc: func(_ context.Context, _ *Context) (Action, error) {
			beforeSendCalled = true
			return ActionContinue, nil
		},
	})

	hctx := testContext()

	// Only run BeforeProcess — BeforeSend hook should not execute.
	_, _ = p.RunBeforeProcess(context.Background(), hctx)

	if !beforeProcessCalled {
		t.Error("before_process hook should have been called")
	}
	if beforeSendCalled {
		t.Error("before_send hook should NOT have been called during RunBeforeProcess")
	}
}
