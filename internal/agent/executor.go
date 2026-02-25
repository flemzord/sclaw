package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/tool"
)

// ToolExecutorConfig holds the dependencies for tool execution.
type ToolExecutorConfig struct {
	Registry        *tool.Registry
	PolicyCfg       tool.PolicyConfig
	PolicyCtx       tool.PolicyContext
	Elevated        *tool.ElevatedState
	Requester       tool.ApprovalRequester
	ApprovalTimeout time.Duration
	Env             tool.ExecutionEnv
}

// ToolExecutor handles parallel tool execution with panic recovery.
type ToolExecutor struct {
	registry        *tool.Registry
	policyCfg       tool.PolicyConfig
	policyCtx       tool.PolicyContext
	elevated        *tool.ElevatedState
	requester       tool.ApprovalRequester
	approvalTimeout time.Duration
	env             tool.ExecutionEnv
}

// NewToolExecutor creates a ToolExecutor from the given configuration.
func NewToolExecutor(cfg ToolExecutorConfig) *ToolExecutor {
	return &ToolExecutor{
		registry:        cfg.Registry,
		policyCfg:       cfg.PolicyCfg,
		policyCtx:       cfg.PolicyCtx,
		elevated:        cfg.Elevated,
		requester:       cfg.Requester,
		approvalTimeout: cfg.ApprovalTimeout,
		env:             cfg.Env,
	}
}

// Execute runs all tool calls in parallel and returns results in input order.
// Panics in individual tools are recovered and reported as error outputs.
func (e *ToolExecutor) Execute(ctx context.Context, calls []provider.ToolCall) []ToolCallRecord {
	results := make([]ToolCallRecord, len(calls))
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, tc provider.ToolCall) {
			defer wg.Done()
			results[idx] = e.executeSingle(ctx, tc)
		}(i, call)
	}

	wg.Wait()
	return results
}

func (e *ToolExecutor) executeSingle(ctx context.Context, tc provider.ToolCall) (record ToolCallRecord) {
	record.ID = tc.ID
	record.Name = tc.Name
	record.Arguments = tc.Arguments

	start := time.Now()

	defer func() {
		record.Duration = time.Since(start)
		if r := recover(); r != nil {
			record.Panicked = true
			record.Output = tool.Output{
				Content: fmt.Sprintf("panic: %v", r),
				IsError: true,
			}
		}
	}()

	out, err := e.registry.Execute(
		ctx,
		tc.Name,
		tc.Arguments,
		e.policyCfg,
		e.policyCtx,
		e.elevated,
		e.requester,
		e.approvalTimeout,
		e.env,
	)
	if err != nil {
		record.Output = tool.Output{
			Content: err.Error(),
			IsError: true,
		}
		return record
	}

	record.Output = out
	return record
}
