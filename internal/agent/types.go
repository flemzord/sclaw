// Package agent implements the ReAct (Reason + Act) reasoning loop
// that transforms user messages into responses through iterative
// provider calls and tool executions.
package agent

import (
	"encoding/json"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/tool"
)

// StopReason describes why the agent loop terminated.
type StopReason string

// StopReason constants for agent loop termination.
const (
	StopReasonComplete      StopReason = "complete"
	StopReasonMaxIterations StopReason = "max_iterations"
	StopReasonLoopDetected  StopReason = "loop_detected"
	StopReasonTokenBudget   StopReason = "token_budget"
	StopReasonTimeout       StopReason = "timeout"
	StopReasonError         StopReason = "error"
)

// ToolCallRecord tracks one tool invocation during the agent loop.
type ToolCallRecord struct {
	ID        string
	Name      string
	Arguments json.RawMessage
	Output    tool.Output
	Duration  time.Duration
	Panicked  bool
}

// StreamEventType identifies the kind of streaming event.
type StreamEventType string

// StreamEventType constants for streaming events.
const (
	StreamEventText      StreamEventType = "text"
	StreamEventToolStart StreamEventType = "tool_start"
	StreamEventToolEnd   StreamEventType = "tool_end"
	StreamEventDone      StreamEventType = "done"
	StreamEventError     StreamEventType = "error"
	StreamEventUsage     StreamEventType = "usage"
)

// StreamEvent is a single event emitted during a streaming agent loop.
type StreamEvent struct {
	Type     StreamEventType
	Content  string
	ToolCall *ToolCallRecord
	Usage    *provider.TokenUsage
	// Final is set on StreamEventDone with the aggregated loop response.
	Final *Response
	Err   error
}

// Request is the input to the agent loop.
type Request struct {
	Messages     []provider.LLMMessage
	SystemPrompt string
	Tools        []provider.ToolDefinition
	Config       LoopConfig
}

// Response is the output of the agent loop.
type Response struct {
	Content    string
	ToolCalls  []ToolCallRecord
	TotalUsage provider.TokenUsage
	Iterations int
	StopReason StopReason
}
