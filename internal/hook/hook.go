// Package hook provides a message lifecycle hook system for the router pipeline.
// Hooks intercept messages at three positions: before processing, before sending,
// and after sending. This enables audit logging, message filtering, and mutation.
package hook

import (
	"context"
	"log/slog"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/pkg/message"
)

// Position identifies where in the pipeline a hook executes.
type Position string

const (
	// BeforeProcess runs after group policy, before the lane lock.
	// Hooks here can drop messages.
	BeforeProcess Position = "before_process"

	// BeforeSend runs after the agent loop, before the response is sent.
	// Hooks here can modify the outbound message.
	BeforeSend Position = "before_send"

	// AfterSend runs after the response has been sent and persisted.
	// Hooks here are fire-and-forget (errors are logged, never propagated).
	AfterSend Position = "after_send"
)

// Action signals the pipeline what to do after a hook executes.
type Action int

const (
	// ActionContinue tells the pipeline to proceed normally.
	ActionContinue Action = iota

	// ActionDrop tells the pipeline to stop processing this message.
	// Only valid for BeforeProcess hooks.
	ActionDrop

	// ActionModify signals that the hook mutated the outbound message.
	// Only meaningful for BeforeSend hooks.
	ActionModify
)

// SessionView provides read-only access to session data for hooks.
// This interface breaks the router→hook circular dependency.
// History is intentionally excluded — hooks don't need conversation
// history (avoids expensive copies).
type SessionView interface {
	SessionID() string
	SessionKey() (channel, chatID, threadID string)
	AgentID() string
	CreatedAt() time.Time
	GetMetadata(key string) (any, bool)
}

// Context carries data available to hooks. Shared across all three
// positions within a single pipeline execution. Uses request struct
// pattern (>3 fields).
type Context struct {
	Position Position
	Inbound  message.InboundMessage
	Session  SessionView

	// Outbound is non-nil for BeforeSend and AfterSend.
	Outbound *message.OutboundMessage

	// Response is non-nil for BeforeSend and AfterSend.
	Response *agent.Response

	// Metadata is shared across all 3 positions, allowing hooks
	// to communicate data through the pipeline.
	Metadata map[string]any

	Logger *slog.Logger
}

// Hook is the extension point interface for pipeline interception.
type Hook interface {
	// Position returns where this hook should execute.
	Position() Position

	// Priority determines execution order within a position.
	// Lower values run first.
	Priority() int

	// Execute runs the hook logic. The returned Action tells the
	// pipeline how to proceed.
	Execute(ctx context.Context, hctx *Context) (Action, error)
}
