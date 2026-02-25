package router

import (
	"context"
	"log/slog"
	"strings"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/hook"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/workspace"
	"github.com/flemzord/sclaw/pkg/message"
)

// defaultMaxHistoryLen is the default maximum number of LLM messages
// kept in a session's history. Oldest entries are trimmed when exceeded.
const defaultMaxHistoryLen = 100

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

	// MaxHistoryLen caps the number of LLM messages kept in a session's
	// history. When exceeded, the oldest entries are trimmed. Zero means
	// use the default (100).
	MaxHistoryLen int

	// SoulProvider loads the agent personality prompt (SOUL.md).
	// Nil → DefaultSoulPrompt.
	SoulProvider workspace.SoulProvider

	// SkillActivator selects which skills to include in the prompt.
	// Nil → no skills.
	SkillActivator *workspace.SkillActivator

	// Skills are the loaded skill definitions (from the workspace skills dir).
	Skills []workspace.Skill

	// HookPipeline runs hooks at before_process, before_send, and after_send.
	// Nil → no hooks (all hook steps become no-ops for backward compatibility).
	HookPipeline *hook.Pipeline
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
	if cfg.MaxHistoryLen <= 0 {
		cfg.MaxHistoryLen = defaultMaxHistoryLen
	}
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
	// Only attempt JSON parsing when the message carries a raw payload.
	if len(env.Message.Raw) > 0 {
		if id, _, ok := p.cfg.ApprovalManager.IsApprovalResponse(env.Message); ok {
			logger.Warn("pipeline: approval response reached pipeline unexpectedly", "approval_id", id)
		}
	}

	// Step 4: Group policy — check if message should be processed.
	if !p.cfg.GroupPolicy.ShouldProcess(env.Message) {
		logger.Debug("pipeline: message filtered by group policy",
			"sender", env.Message.Sender.ID,
		)
		return PipelineResult{Session: session, Skipped: true}
	}

	// Step 5: Hook before-process — intercept/drop messages.
	// hookMeta is shared across all 3 hook positions within this execution.
	hookMeta := make(map[string]any)
	if p.cfg.HookPipeline != nil {
		hctx := &hook.Context{
			Position: hook.BeforeProcess,
			Inbound:  env.Message,
			Session:  &sessionViewAdapter{s: session},
			Metadata: hookMeta,
			Logger:   logger,
		}
		action, _ := p.cfg.HookPipeline.RunBeforeProcess(ctx, hctx)
		if action == hook.ActionDrop {
			return PipelineResult{Session: session, Skipped: true}
		}
	}

	// Step 6: Lane lock acquire (step 15 releases via defer).
	p.cfg.LaneLock.Acquire(env.Key)
	defer p.cfg.LaneLock.Release(env.Key) // Step 15

	// Step 7: Agent resolution — get or create agent loop for this session.
	// Called after lane lock acquisition to avoid a data race on the live
	// session pointer (R1 fix).
	loop, err := p.cfg.AgentFactory.ForSession(session)
	if err != nil {
		p.sendError(ctx, env.Message, "Failed to initialize agent.")
		return PipelineResult{Session: session, Error: err}
	}

	// Step 8: History — append user message to session history.
	llmMsg := messageToLLM(env.Message)
	session.History = append(session.History, llmMsg)

	// Trim history to MaxHistoryLen to prevent unbounded growth.
	if limit := p.cfg.MaxHistoryLen; len(session.History) > limit {
		session.History = session.History[len(session.History)-limit:]
	}

	// Step 9: Context — build agent request.
	systemParts := p.buildSystemParts(env.Message)
	req := agent.Request{
		Messages:     session.History,
		SystemPrompt: strings.Join(systemParts, "\n\n"),
	}

	// Step 10: Agent loop — run the agent.
	resp, err := loop.Run(ctx, req)
	if err != nil {
		logger.Error("pipeline: agent loop failed", "error", err, "session_id", session.ID)
		p.sendError(ctx, env.Message, "An error occurred while processing your message.")
		return PipelineResult{Session: session, Error: err}
	}

	// Step 11: Hook before-send — allow hooks to modify outbound.
	outbound := buildOutbound(env.Message, resp)
	if p.cfg.HookPipeline != nil {
		hctx := &hook.Context{
			Position: hook.BeforeSend,
			Inbound:  env.Message,
			Session:  &sessionViewAdapter{s: session},
			Outbound: &outbound,
			Response: &resp,
			Metadata: hookMeta,
			Logger:   logger,
		}
		_, _ = p.cfg.HookPipeline.RunBeforeSend(ctx, hctx)
	}

	// Step 12: Send — deliver response via ResponseSender.
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

	// Step 14: Hook after-send — fire-and-forget (audit, analytics, etc.).
	if p.cfg.HookPipeline != nil {
		hctx := &hook.Context{
			Position: hook.AfterSend,
			Inbound:  env.Message,
			Session:  &sessionViewAdapter{s: session},
			Outbound: &outbound,
			Response: &resp,
			Metadata: hookMeta,
			Logger:   logger,
		}
		p.cfg.HookPipeline.RunAfterSend(ctx, hctx)
	}

	// Lazy pruning — opportunistically prune stale sessions.
	if p.cfg.Pruner != nil {
		if pruned := p.cfg.Pruner.TryPrune(); pruned > 0 {
			logger.Info("pipeline: pruned stale sessions", "count", pruned)
		}
	}

	return PipelineResult{Session: session, Response: &resp}
}

// buildSystemParts assembles the system prompt parts from SOUL.md and active skills.
func (p *Pipeline) buildSystemParts(msg message.InboundMessage) []string {
	var parts []string

	// Load SOUL.md content (personality prompt).
	soulContent := workspace.DefaultSoulPrompt
	if p.cfg.SoulProvider != nil {
		if content, err := p.cfg.SoulProvider.Load(); err == nil && content != "" {
			soulContent = content
		}
	}
	parts = append(parts, soulContent)

	// Activate and format skills.
	if p.cfg.SkillActivator != nil && len(p.cfg.Skills) > 0 {
		active := p.cfg.SkillActivator.Activate(workspace.ActivateRequest{
			Skills:      p.cfg.Skills,
			UserMessage: msg.TextContent(),
		})
		if formatted := workspace.FormatSkillsForPrompt(active); formatted != "" {
			parts = append(parts, formatted)
		}
	}

	return parts
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
