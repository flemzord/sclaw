package router

import (
	"context"
	"log/slog"
	"strings"

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

	// StreamSender, if non-nil, delivers streaming responses to channels
	// that support progressive message editing (e.g. Telegram edit-in-place).
	// Nil means streaming is disabled globally (backward compatible).
	StreamSender StreamSender

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

	// SkillResolver, if non-nil, provides per-agent skill sections that are
	// appended to the system prompt. Nil means no skills (backward compatible).
	SkillResolver SkillResolver
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

	// Step 9a: Skill resolution — append active skills to the system prompt.
	if p.cfg.SkillResolver != nil && session.AgentID != "" {
		if skillSection, err := p.cfg.SkillResolver.ResolveSkills(session.AgentID, env.Message.TextContent()); err == nil && skillSection != "" {
			systemPrompt += "\n\n" + skillSection
		} else if err != nil {
			logger.Warn("pipeline: failed to resolve skills",
				"session_id", session.ID, "agent_id", session.AgentID, "error", err)
		}
	}

	// Step 9c: Workspace context — tell the LLM which directory it operates in
	// so it can resolve absolute paths and use tools like read_file correctly.
	if ws := loop.Workspace(); ws != "" {
		systemPrompt += "\n\nYour workspace directory is: " + ws +
			"\nAll file operations (read_file, write_file) and command execution (exec) operate within this directory. " +
			"You can use both relative and absolute paths as long as they resolve within this workspace."
	}

	req := agent.Request{
		Messages:     session.History,
		SystemPrompt: systemPrompt,
		Tools:        loop.ToolDefinitions(),
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

	// Step 10: Agent loop — run synchronously or stream depending on config.
	if session.StreamingEnabled && p.cfg.StreamSender != nil {
		return p.executeStreaming(ctx, env, session, loop, req, cancelTyping, hookMeta, logger)
	}

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

	return p.finalize(ctx, env, session, resp, hookMeta, logger)
}

// finalize handles Steps 13–14 (persistence, hooks, pruning) after a response
// has been delivered, regardless of whether it was sent synchronously or streamed.
func (p *Pipeline) finalize(
	ctx context.Context,
	env envelope,
	session *Session,
	resp agent.Response,
	hookMeta map[string]any,
	logger *slog.Logger,
) PipelineResult {
	outbound := buildOutbound(env.Message, resp)

	// Step 13: Persistence — save assistant response to history and touch session.
	assistantMsg := provider.LLMMessage{
		Role:    provider.MessageRoleAssistant,
		Content: resp.Content,
	}
	session.History = append(session.History, assistantMsg)
	p.cfg.Store.Touch(env.Key)

	// m-60: Trim history after appending assistant message.
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

// executeStreaming runs the agent in streaming mode and delivers chunks
// progressively to the channel. Falls back to the synchronous path if
// RunStream fails or the channel does not support streaming.
func (p *Pipeline) executeStreaming(
	ctx context.Context,
	env envelope,
	session *Session,
	loop *agent.Loop,
	req agent.Request,
	cancelTyping context.CancelFunc,
	hookMeta map[string]any,
	logger *slog.Logger,
) PipelineResult {
	streamCh, err := loop.RunStream(ctx, req)
	if err != nil {
		// Fallback to synchronous path on stream init error.
		logger.Warn("pipeline: RunStream failed, falling back to sync",
			"session_id", session.ID, "error", err)
		return p.executeSyncFallback(ctx, env, session, loop, req, cancelTyping, hookMeta, logger)
	}

	// Build the outbound message shell for the stream sender.
	outbound := message.OutboundMessage{
		Channel:  env.Message.Channel,
		Chat:     env.Message.Chat,
		ThreadID: env.Message.ThreadID,
	}

	// textCh bridges StreamEvent text chunks to the StreamSender.
	textCh := make(chan string, 4)

	// Start the stream sender in a goroutine.
	type sendResult struct {
		streamed bool
		err      error
	}
	sendDone := make(chan sendResult, 1)
	go func() {
		streamed, err := p.cfg.StreamSender.SendStream(ctx, outbound, textCh)
		sendDone <- sendResult{streamed: streamed, err: err}
	}()

	// Drain the agent stream, forwarding text chunks to textCh.
	var builder strings.Builder
	var final *agent.Response
	var streamErr error

	for event := range streamCh {
		switch event.Type {
		case agent.StreamEventText:
			builder.WriteString(event.Content)
			// Non-blocking send to textCh; drop chunk if buffer full
			// to avoid blocking the agent loop. The final text is sent
			// via the before_send hook correction path if needed.
			select {
			case textCh <- event.Content:
			default:
			}

		case agent.StreamEventToolStart:
			logger.Debug("pipeline: stream tool start",
				"session_id", session.ID,
				"tool", event.ToolCall.Name)

		case agent.StreamEventToolEnd:
			logger.Debug("pipeline: stream tool end",
				"session_id", session.ID,
				"tool", event.ToolCall.Name)

		case agent.StreamEventDone:
			final = event.Final

		case agent.StreamEventError:
			streamErr = event.Err

		case agent.StreamEventUsage:
			// Usage tracking is handled internally by the agent loop.
		}
	}

	close(textCh)

	// Stop typing indicator.
	if cancelTyping != nil {
		cancelTyping()
	}

	// Wait for SendStream to finish.
	sr := <-sendDone

	if streamErr != nil {
		logger.Error("pipeline: agent stream error",
			"error", streamErr, "session_id", session.ID)
		p.sendError(ctx, env.Message, "An error occurred while processing your message.")
		return PipelineResult{Session: session, Error: streamErr}
	}

	// Build the response from the final event or accumulated text.
	var resp agent.Response
	if final != nil {
		resp = *final
	} else {
		resp = agent.Response{Content: builder.String()}
	}

	// If the channel did not support streaming, fall back to a regular Send.
	if !sr.streamed {
		outFull := buildOutbound(env.Message, resp)

		// Run before_send hook.
		if p.cfg.HookPipeline != nil {
			hctx := &hook.Context{
				Position: hook.BeforeSend,
				Inbound:  env.Message,
				Outbound: &outFull,
				Response: &resp,
				Session:  &sessionViewAdapter{session: session},
				Metadata: hookMeta,
				Logger:   logger,
			}
			if _, err := p.cfg.HookPipeline.RunBeforeSend(ctx, hctx); err != nil {
				logger.Warn("pipeline: hook before_send error", "error", err)
			}
		}

		if err := p.cfg.ResponseSender.Send(ctx, outFull); err != nil {
			logger.Error("pipeline: failed to send response (stream fallback)",
				"error", err, "session_id", session.ID)
			return PipelineResult{Session: session, Response: &resp, Error: err}
		}
		return p.finalize(ctx, env, session, resp, hookMeta, logger)
	}

	if sr.err != nil {
		logger.Error("pipeline: stream send error",
			"error", sr.err, "session_id", session.ID)
		return PipelineResult{Session: session, Response: &resp, Error: sr.err}
	}

	// Run before_send hook with the final accumulated text.
	// If the hook modifies the response, send a corrective message.
	if p.cfg.HookPipeline != nil {
		outFull := buildOutbound(env.Message, resp)
		originalContent := resp.Content
		hctx := &hook.Context{
			Position: hook.BeforeSend,
			Inbound:  env.Message,
			Outbound: &outFull,
			Response: &resp,
			Session:  &sessionViewAdapter{session: session},
			Metadata: hookMeta,
			Logger:   logger,
		}
		if _, err := p.cfg.HookPipeline.RunBeforeSend(ctx, hctx); err != nil {
			logger.Warn("pipeline: hook before_send error", "error", err)
		}
		// If hook modified the content, send a corrective message.
		if resp.Content != originalContent {
			corrective := buildOutbound(env.Message, resp)
			if err := p.cfg.ResponseSender.Send(ctx, corrective); err != nil {
				logger.Warn("pipeline: failed to send hook-corrected message",
					"error", err, "session_id", session.ID)
			}
		}
	}

	return p.finalize(ctx, env, session, resp, hookMeta, logger)
}

// executeSyncFallback runs the synchronous agent path when streaming
// initialization fails.
func (p *Pipeline) executeSyncFallback(
	ctx context.Context,
	env envelope,
	session *Session,
	loop *agent.Loop,
	req agent.Request,
	cancelTyping context.CancelFunc,
	hookMeta map[string]any,
	logger *slog.Logger,
) PipelineResult {
	resp, err := loop.Run(ctx, req)

	if cancelTyping != nil {
		cancelTyping()
	}

	if err != nil {
		logger.Error("pipeline: agent loop failed (sync fallback)",
			"error", err, "session_id", session.ID)
		p.sendError(ctx, env.Message, "An error occurred while processing your message.")
		return PipelineResult{Session: session, Error: err}
	}

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

	if err := p.cfg.ResponseSender.Send(ctx, outbound); err != nil {
		logger.Error("pipeline: failed to send response (sync fallback)",
			"error", err, "session_id", session.ID)
		return PipelineResult{Session: session, Response: &resp, Error: err}
	}

	return p.finalize(ctx, env, session, resp, hookMeta, logger)
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
