package router

import (
	"context"
	"log/slog"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/hook"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/security"
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
	HookPipeline    *hook.Pipeline
	Logger          *slog.Logger

	// ChannelLookup resolves channels by name, used to start typing
	// indicators while the agent is processing. Nil means no typing.
	ChannelLookup ChannelLookup

	// AuditLogger, if non-nil, emits session lifecycle events (session_create).
	// session_delete events are emitted by the admin handler.
	// TODO: wire session_delete audit events from the admin handler.
	AuditLogger *security.AuditLogger

	// MaxHistoryLen caps the number of LLM messages kept in a session's
	// history. When exceeded, the oldest entries are trimmed. Zero means
	// use the default (100).
	MaxHistoryLen int

	// HistoryResolver, if non-nil, provides per-agent persistent storage.
	// When set, history is restored from SQLite on new sessions and
	// persisted after each user/assistant message (write-behind, non-fatal).
	HistoryResolver HistoryResolver

	// SoulResolver, if non-nil, provides per-agent system prompts loaded
	// from SOUL.md files. Nil means use the default prompt (backward compatible).
	SoulResolver SoulResolver
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
	if session == nil {
		logger.Warn("pipeline: max sessions reached, message dropped",
			"channel", env.Key.Channel,
			"chat_id", env.Key.ChatID,
		)
		p.sendError(ctx, env.Message, "Too many active sessions. Please try again later.")
		return PipelineResult{Skipped: true}
	}
	if created {
		logger.Info("pipeline: new session created", "session_id", session.ID)
		if p.cfg.AuditLogger != nil {
			p.cfg.AuditLogger.Log(security.AuditEvent{
				Type:      security.EventSessionCreate,
				SessionID: session.ID,
				Channel:   env.Key.Channel,
				ChatID:    env.Key.ChatID,
			})
		}
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

	// Step 5: Lane lock acquire (step 15 releases via defer).
	// C-13 fix: Lane lock is acquired BEFORE hook before_process so that
	// the session pointer is protected by the lane lock when hooks access it.
	p.cfg.LaneLock.Acquire(env.Key)
	defer p.cfg.LaneLock.Release(env.Key) // Step 15

	// Step 6: Hook before-process — run after lane lock acquisition (C-13 fix).
	hookMeta := make(map[string]any)
	if p.cfg.HookPipeline != nil {
		hctx := &hook.Context{
			Position: hook.BeforeProcess,
			Inbound:  env.Message,
			Session:  &sessionViewAdapter{session: session},
			Metadata: hookMeta,
			Logger:   logger,
		}
		// m-29: Log the error if non-nil instead of ignoring it.
		action, err := p.cfg.HookPipeline.RunBeforeProcess(ctx, hctx)
		if err != nil {
			logger.Warn("pipeline: hook before_process error", "error", err)
		}
		if action == hook.ActionDrop {
			return PipelineResult{Session: session, Skipped: true}
		}
	}

	// Step 7: Agent resolution — get or create agent loop for this session.
	// Called after lane lock acquisition to avoid a data race on the live
	// session pointer (R1 fix).
	loop, err := p.cfg.AgentFactory.ForSession(session, env.Message)
	if err != nil {
		logger.Error("pipeline: agent initialization failed", "error", err, "session_id", session.ID, "agent_id", session.AgentID)
		p.sendError(ctx, env.Message, "Failed to initialize agent.")
		return PipelineResult{Session: session, Error: err}
	}

	// Step 7b: History restore — if the session was just created and a
	// persistent store is available, restore previous history from SQLite.
	if created && p.cfg.HistoryResolver != nil && session.AgentID != "" {
		if store := p.cfg.HistoryResolver.ResolveHistory(session.AgentID); store != nil {
			pKey := persistenceKey(env.Key)
			restored, err := store.GetRecent(pKey, p.cfg.MaxHistoryLen)
			if err != nil {
				logger.Warn("pipeline: failed to restore history from SQLite",
					"session_id", session.ID, "error", err)
			} else if len(restored) > 0 {
				session.History = restored
				logger.Info("pipeline: restored history from SQLite",
					"session_id", session.ID, "messages", len(restored))
			}
		}
	}

	// Step 8: History — append user message to session history.
	llmMsg := messageToLLM(env.Message)
	session.History = append(session.History, llmMsg)

	// Trim history to MaxHistoryLen to prevent unbounded growth.
	if limit := p.cfg.MaxHistoryLen; len(session.History) > limit {
		session.History = session.History[len(session.History)-limit:]
	}

	// Step 8b: Persist user message to SQLite (write-behind, non-fatal).
	if p.cfg.HistoryResolver != nil && session.AgentID != "" {
		if store := p.cfg.HistoryResolver.ResolveHistory(session.AgentID); store != nil {
			pKey := persistenceKey(env.Key)
			if err := store.Append(pKey, llmMsg); err != nil {
				logger.Warn("pipeline: failed to persist user message",
					"session_id", session.ID, "error", err)
			}
		}
	}

	// Step 9: Context — build agent request.
	systemPrompt := workspace.DefaultSoulPrompt
	if p.cfg.SoulResolver != nil && session.AgentID != "" {
		if s, err := p.cfg.SoulResolver.ResolveSoul(session.AgentID); err == nil {
			systemPrompt = s
		} else {
			logger.Warn("pipeline: failed to load soul prompt",
				"session_id", session.ID, "agent_id", session.AgentID, "error", err)
		}
	}
	req := agent.Request{
		Messages:     session.History,
		SystemPrompt: systemPrompt,
	}

	// Step 9b: Typing indicator — show "typing..." while agent processes.
	var cancelTyping context.CancelFunc
	if p.cfg.ChannelLookup != nil {
		if ch, ok := p.cfg.ChannelLookup.Get(env.Key.Channel); ok {
			if tc, ok := ch.(channel.TypingChannel); ok {
				typingCtx, cancel := context.WithCancel(ctx)
				cancelTyping = cancel
				channel.StartTypingLoop(typingCtx, tc, env.Message.Chat, 0)
			}
		}
	}

	// Step 10: Agent loop — run the agent.
	resp, err := loop.Run(ctx, req)

	// Stop typing indicator before handling the result.
	if cancelTyping != nil {
		cancelTyping()
	}

	if err != nil {
		logger.Error("pipeline: agent loop failed", "error", err, "session_id", session.ID)
		p.sendError(ctx, env.Message, "An error occurred while processing your message.")
		return PipelineResult{Session: session, Error: err}
	}

	// Step 11: Hook before-send.
	outbound := buildOutbound(env.Message, resp)
	if p.cfg.HookPipeline != nil {
		hctx := &hook.Context{
			Position: hook.BeforeSend,
			Inbound:  env.Message,
			Outbound: &outbound,
			Response: &resp,
			Session:  &sessionViewAdapter{session: session},
			Metadata: hookMeta,
			Logger:   logger,
		}
		if _, err := p.cfg.HookPipeline.RunBeforeSend(ctx, hctx); err != nil {
			logger.Warn("pipeline: hook before_send error", "error", err)
		}
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

	// m-60: Trim history after appending assistant message. After Step 13,
	// history may exceed MaxHistoryLen by 1. Trim it here to maintain the invariant.
	if limit := p.cfg.MaxHistoryLen; len(session.History) > limit {
		session.History = session.History[len(session.History)-limit:]
	}

	// Step 13b: Persist assistant message to SQLite (write-behind, non-fatal).
	if p.cfg.HistoryResolver != nil && session.AgentID != "" {
		if store := p.cfg.HistoryResolver.ResolveHistory(session.AgentID); store != nil {
			pKey := persistenceKey(env.Key)
			if err := store.Append(pKey, assistantMsg); err != nil {
				logger.Warn("pipeline: failed to persist assistant message",
					"session_id", session.ID, "error", err)
			}
		}
	}

	// Step 14: Hook after-send (fire-and-forget).
	if p.cfg.HookPipeline != nil {
		hctx := &hook.Context{
			Position: hook.AfterSend,
			Inbound:  env.Message,
			Outbound: &outbound,
			Response: &resp,
			Session:  &sessionViewAdapter{session: session},
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

// sendError sends a user-friendly error message via ResponseSender. Never panics.
func (p *Pipeline) sendError(ctx context.Context, original message.InboundMessage, text string) {
	errMsg := message.NewTextMessage(original.Chat, text)
	errMsg.Channel = original.Channel
	errMsg.ThreadID = original.ThreadID
	errMsg.ReplyToID = original.ID
	if err := p.cfg.ResponseSender.Send(ctx, errMsg); err != nil {
		if p.cfg.Logger != nil {
			p.cfg.Logger.Error("pipeline: failed to send error message", "error", err)
		}
	}
}
