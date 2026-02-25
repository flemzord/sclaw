package router

import (
	"context"
	"log/slog"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/pkg/message"
)

// PipelineConfig groups the dependencies for the 15-step pipeline.
// Uses request struct pattern for >3 parameters.
type PipelineConfig struct {
	Store           SessionStore
	LaneLock        *LaneLock
	GroupPolicy     GroupPolicy
	ApprovalManager *ApprovalManager
	AgentFactory    AgentFactory
	ResponseSender  ResponseSender
	Pruner          *lazyPruner
	Logger          *slog.Logger
}

// PipelineResult contains the outcome of pipeline execution.
type PipelineResult struct {
	Session  *Session
	Response *agent.Response
	Error    error
	Skipped  bool
}

// Pipeline executes the 15-step message processing pipeline.
type Pipeline struct {
	cfg PipelineConfig
}

// NewPipeline creates a new pipeline with the given configuration.
func NewPipeline(cfg PipelineConfig) *Pipeline {
	return &Pipeline{cfg: cfg}
}

// Execute runs the 15-step pipeline for a single message.
func (p *Pipeline) Execute(ctx context.Context, env envelope) PipelineResult {
	logger := p.cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Step 1: Reception — log the incoming message.
	logger.Info("pipeline: message received",
		"channel", env.Key.Channel,
		"chat_id", env.Key.ChatID,
		"thread_id", env.Key.ThreadID,
	)

	// Step 2: Session resolution — get or create session.
	session, created := p.cfg.Store.GetOrCreate(env.Key)
	if created {
		logger.Info("pipeline: new session created", "session_id", session.ID)
	}

	// Step 3: Approval interception — safety net.
	// If this message is an approval response, it should have been intercepted
	// by Router.Submit() before reaching the pipeline. Log a warning if it wasn't.
	if id, _, ok := p.cfg.ApprovalManager.IsApprovalResponse(env.Message); ok {
		logger.Warn("pipeline: approval response reached pipeline unexpectedly", "approval_id", id)
	}

	// Step 4: Agent resolution — get or create agent loop for this session.
	loop, err := p.cfg.AgentFactory.ForSession(session)
	if err != nil {
		p.sendError(ctx, env.Message, "Failed to initialize agent.")
		return PipelineResult{Session: session, Error: err}
	}

	// Step 5: Group policy — check if message should be processed.
	if !p.cfg.GroupPolicy.ShouldProcess(env.Message) {
		logger.Debug("pipeline: message filtered by group policy",
			"sender", env.Message.Sender.ID,
		)
		return PipelineResult{Session: session, Skipped: true}
	}

	// Step 6: Hook before-process — placeholder, no-op.
	// Will be implemented when the hook system is added.

	// Step 7: Lane lock acquire (step 15 releases via defer).
	p.cfg.LaneLock.Acquire(env.Key)
	defer p.cfg.LaneLock.Release(env.Key) // Step 15

	// Step 8: History — append user message to session history.
	llmMsg := messageToLLM(env.Message)
	session.History = append(session.History, llmMsg)

	// Step 9: Context — build agent request.
	// Partial placeholder: uses a hardcoded system prompt for now.
	req := agent.Request{
		Messages:     session.History,
		SystemPrompt: "You are a helpful assistant.", // Placeholder — will come from config/context engine.
	}

	// Step 10: Agent loop — run the agent.
	resp, err := loop.Run(ctx, req)
	if err != nil {
		logger.Error("pipeline: agent loop failed", "error", err, "session_id", session.ID)
		p.sendError(ctx, env.Message, "An error occurred while processing your message.")
		return PipelineResult{Session: session, Error: err}
	}

	// Step 11: Hook before-send — placeholder, no-op.

	// Step 12: Send — deliver response via ResponseSender.
	outbound := buildOutbound(env.Message, resp)
	if err := p.cfg.ResponseSender.Send(ctx, outbound); err != nil {
		logger.Error("pipeline: failed to send response", "error", err, "session_id", session.ID)
		return PipelineResult{Session: session, Response: &resp, Error: err}
	}

	// Step 13: Persistence — save assistant response to history and touch session.
	assistantMsg := provider.LLMMessage{
		Role:    provider.MessageRoleAssistant,
		Content: resp.Content,
	}
	session.History = append(session.History, assistantMsg)
	p.cfg.Store.Touch(env.Key)

	// Step 14: Hook after-send — placeholder, no-op.

	// Lazy pruning — opportunistically prune stale sessions.
	if p.cfg.Pruner != nil {
		if pruned := p.cfg.Pruner.TryPrune(); pruned > 0 {
			logger.Info("pipeline: pruned stale sessions", "count", pruned)
		}
	}

	return PipelineResult{Session: session, Response: &resp}
}

// sendError sends a user-friendly error message via ResponseSender. Never panics.
func (p *Pipeline) sendError(ctx context.Context, original message.InboundMessage, text string) {
	errMsg := message.NewTextMessage(original.Chat, text)
	errMsg.ThreadID = original.ThreadID
	errMsg.ReplyToID = original.ID
	if err := p.cfg.ResponseSender.Send(ctx, errMsg); err != nil {
		if p.cfg.Logger != nil {
			p.cfg.Logger.Error("pipeline: failed to send error message", "error", err)
		}
	}
}
