package telegram

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/pkg/message"
)

const (
	maxConsecutivePollingErrors = 5
	errorPauseDuration          = 30 * time.Second
)

// Poller implements long-polling for receiving Telegram updates.
type Poller struct {
	client      *Client
	inbox       func(message.InboundMessage) error
	allowList   *channel.AllowList
	logger      *slog.Logger
	botUsername string
	channelName string
	config      Config

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// NewPoller creates a new Poller.
func NewPoller(client *Client, inbox func(message.InboundMessage) error, allowList *channel.AllowList, logger *slog.Logger, botUsername, channelName string, config Config) *Poller {
	ctx, cancel := context.WithCancel(context.Background())
	return &Poller{
		client:      client,
		inbox:       inbox,
		allowList:   allowList,
		logger:      logger,
		botUsername: botUsername,
		channelName: channelName,
		config:      config,
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
	}
}

// Start launches the polling loop in a goroutine.
func (p *Poller) Start() {
	go p.loop()
}

// Stop signals the polling loop to stop and waits for it to finish.
// It respects the provided context deadline — if ctx expires before the
// loop exits, Stop returns ctx.Err().
// It is safe to call Stop multiple times.
func (p *Poller) Stop(ctx context.Context) error {
	p.once.Do(func() { p.cancel() })
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loop runs the long-polling loop until Stop() is called.
func (p *Poller) loop() {
	defer close(p.done)

	var offset int
	var consecutiveErrors int

	for {
		if p.ctx.Err() != nil {
			return
		}

		// Use a per-request timeout for the long-polling call.
		// PollingTimeout is the server-side wait; add 10s margin for network.
		reqTimeout := time.Duration(p.config.PollingTimeout+10) * time.Second
		reqCtx, reqCancel := context.WithTimeout(p.ctx, reqTimeout)

		updates, err := p.client.GetUpdates(reqCtx, GetUpdatesRequest{
			Offset:         offset,
			Timeout:        p.config.PollingTimeout,
			AllowedUpdates: p.config.AllowedUpdates,
		})
		reqCancel()

		if err != nil {
			// Don't log when the poller is being stopped.
			if p.ctx.Err() != nil {
				return
			}

			consecutiveErrors++
			p.logger.Error("polling getUpdates failed",
				"error", err,
				"consecutive_errors", consecutiveErrors,
			)

			// Progressive backoff: 1s, 2s, 3s, 4s, then pause 30s at threshold.
			if consecutiveErrors >= maxConsecutivePollingErrors {
				p.logger.Warn("polling paused after consecutive errors",
					"pause", errorPauseDuration,
				)
				select {
				case <-p.ctx.Done():
					return
				case <-time.After(errorPauseDuration):
				}
				consecutiveErrors = 0
			} else {
				backoff := time.Duration(consecutiveErrors) * time.Second
				select {
				case <-p.ctx.Done():
					return
				case <-time.After(backoff):
				}
			}
			continue
		}

		consecutiveErrors = 0

		for _, update := range updates {
			offset = update.UpdateID + 1
			p.handleUpdate(&update)
		}
	}
}

// handleUpdate processes a single update.
func (p *Poller) handleUpdate(update *Update) {
	p.logger.Debug("received update", "update_id", update.UpdateID)

	msg, err := convertInbound(update, p.botUsername, p.channelName)
	if err != nil {
		p.logger.Debug("skipping update", "update_id", update.UpdateID, "reason", err)
		return
	}

	p.logger.Debug("inbound message converted",
		"update_id", update.UpdateID,
		"msg_id", msg.ID,
		"sender", msg.Sender.ID,
		"sender_name", msg.Sender.DisplayName,
		"chat_id", msg.Chat.ID,
		"chat_type", msg.Chat.Type,
		"blocks", len(msg.Blocks),
	)

	if msg.HasMedia() {
		if err := resolveMediaURLs(p.ctx, p.client, &msg); err != nil {
			p.logger.Warn("failed to resolve media URLs",
				"update_id", update.UpdateID, "error", err)
		}
	}

	if !p.allowList.IsAllowed(msg) {
		p.logger.Debug("update denied by allow list",
			"update_id", update.UpdateID,
			"sender", msg.Sender.ID,
			"chat", msg.Chat.ID,
		)
		return
	}

	if err := p.inbox(msg); err != nil {
		p.logger.Error("failed to deliver update to inbox",
			"update_id", update.UpdateID,
			"error", err,
		)
	}
}
