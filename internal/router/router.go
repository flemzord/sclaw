package router

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flemzord/sclaw/internal/hook"
	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/pkg/message"
)

const (
	defaultInboxSize = 256
	defaultMaxIdle   = 30 * time.Minute
)

// Config holds the configuration for a Router.
type Config struct {
	WorkerCount    int
	InboxSize      int
	MaxIdle        time.Duration
	GroupPolicy    GroupPolicy
	AgentFactory   AgentFactory
	ResponseSender ResponseSender
	Logger         *slog.Logger

	// HookPipeline runs hooks at before_process, before_send, and after_send.
	// Nil → no hooks (backward compatible).
	HookPipeline *hook.Pipeline

	// RateLimiter, if non-nil, enforces message and session rate limits.
	RateLimiter *security.RateLimiter

	// MaxMessageSize is the maximum allowed message size in bytes.
	// Zero means use the default (1 MiB).
	MaxMessageSize int
}

// withDefaults returns a copy of the config with zero values replaced by defaults.
func (c Config) withDefaults() Config {
	if c.WorkerCount <= 0 {
		c.WorkerCount = DefaultWorkerCount
	}
	if c.InboxSize <= 0 {
		c.InboxSize = defaultInboxSize
	}
	if c.MaxIdle <= 0 {
		c.MaxIdle = defaultMaxIdle
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Router is the central dispatch layer. It maintains sessions, dispatches
// incoming messages through the pipeline to the agent loop, and sends
// responses back via the correct channel.
type Router struct {
	config          Config
	inbox           chan envelope
	inboxMu         sync.RWMutex
	store           SessionStore
	laneLock        *LaneLock
	pool            *WorkerPool
	pipeline        *Pipeline
	approvalManager *ApprovalManager
	pruner          *lazyPruner
	cancel          context.CancelFunc
	stopOnce        sync.Once
	logger          *slog.Logger
	stopped         atomic.Bool
}

// NewRouter creates a new Router with the given configuration.
func NewRouter(cfg Config) (*Router, error) {
	cfg = cfg.withDefaults()

	if cfg.AgentFactory == nil {
		return nil, ErrNoAgentFactory
	}
	if cfg.ResponseSender == nil {
		return nil, ErrNoResponseSender
	}

	store := NewInMemorySessionStore()
	laneLock := NewLaneLock()
	approvalMgr := NewApprovalManager()
	pruner := newLazyPruner(store, laneLock, cfg.MaxIdle)

	pipeline := NewPipeline(PipelineConfig{
		Store:           store,
		LaneLock:        laneLock,
		GroupPolicy:     cfg.GroupPolicy,
		ApprovalManager: approvalMgr,
		AgentFactory:    cfg.AgentFactory,
		ResponseSender:  cfg.ResponseSender,
		Pruner:          pruner,
		Logger:          cfg.Logger,
		HookPipeline:    cfg.HookPipeline,
	})

	return &Router{
		config:          cfg,
		inbox:           make(chan envelope, cfg.InboxSize),
		store:           store,
		laneLock:        laneLock,
		pool:            NewWorkerPool(cfg.WorkerCount),
		pipeline:        pipeline,
		approvalManager: approvalMgr,
		pruner:          pruner,
		logger:          cfg.Logger,
	}, nil
}

// Start launches the worker pool and begins processing messages.
func (r *Router) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	r.inboxMu.Lock()
	if r.stopped.Load() {
		r.inboxMu.Unlock()
		cancel()
		r.logger.Warn("router: start ignored, router already stopped")
		return
	}
	r.cancel = cancel
	r.inboxMu.Unlock()

	r.pool.Start(ctx, r.inbox, func(ctx context.Context, env envelope) {
		r.pipeline.Execute(ctx, env)
	})
	r.logger.Info("router: started", "workers", r.config.WorkerCount, "inbox_size", r.config.InboxSize)
}

// Submit enqueues an inbound message for processing.
// It first checks if the message is an approval response (bypass path).
// If the inbox is full, the message is dropped with a warning log.
func (r *Router) Submit(msg message.InboundMessage) error {
	r.inboxMu.RLock()
	defer r.inboxMu.RUnlock()

	if r.stopped.Load() {
		return ErrRouterStopped
	}

	// Approval bypass: resolve directly without entering inbox or lane lock.
	if id, resp, ok := r.approvalManager.IsApprovalResponse(msg); ok {
		if r.approvalManager.Resolve(id, resp) {
			r.logger.Info("router: approval resolved", "approval_id", id)
			return nil
		}
		r.logger.Warn("router: approval not found or already resolved", "approval_id", id)
	}

	// Validate message size and JSON depth at the system boundary.
	if len(msg.Raw) > 0 {
		if err := security.ValidateMessageSize(msg.Raw, r.config.MaxMessageSize); err != nil {
			r.logger.Warn("router: message too large, rejected",
				"size", len(msg.Raw),
				"channel", msg.Channel,
			)
			return err
		}
		if err := security.ValidateJSONDepth(msg.Raw, 0); err != nil {
			r.logger.Warn("router: message JSON too deep, rejected",
				"channel", msg.Channel,
			)
			return err
		}
	}

	// Rate limit check for messages.
	if r.config.RateLimiter != nil {
		if err := r.config.RateLimiter.Allow("message"); err != nil {
			r.logger.Warn("router: message rate limited",
				"channel", msg.Channel,
				"chat_id", msg.Chat.ID,
			)
			return err
		}
	}

	key := SessionKeyFromMessage(msg)
	env := envelope{Message: msg, Key: key}

	// Non-blocking send — drop with warning if inbox is full.
	select {
	case r.inbox <- env:
		return nil
	default:
		r.logger.Warn("router: inbox full, message dropped",
			"channel", key.Channel,
			"chat_id", key.ChatID,
		)
		return ErrInboxFull
	}
}

// Stop gracefully shuts down the router: closes inbox, drains workers, cancels context.
func (r *Router) Stop(_ context.Context) {
	r.stopOnce.Do(func() {
		r.logger.Info("router: stopping")

		r.inboxMu.Lock()
		r.stopped.Store(true)
		close(r.inbox)
		cancel := r.cancel
		r.inboxMu.Unlock()

		// Cancel before waiting so in-flight handlers can terminate.
		if cancel != nil {
			cancel()
		}

		r.pool.Wait()
		r.logger.Info("router: stopped")
	})
}

// PruneSessions triggers a lazy session prune.
func (r *Router) PruneSessions() int {
	return r.pruner.TryPrune()
}

// Sessions returns the session store for external inspection.
func (r *Router) Sessions() SessionStore {
	return r.store
}
