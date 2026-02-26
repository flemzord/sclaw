package telegram

import (
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
	stopCh      chan struct{}
	done        chan struct{}
	stopOnce    sync.Once
}

// NewPoller creates a new Poller.
func NewPoller(client *Client, inbox func(message.InboundMessage) error, allowList *channel.AllowList, logger *slog.Logger, botUsername, channelName string, config Config) *Poller {
	return &Poller{
		client:      client,
		inbox:       inbox,
		allowList:   allowList,
		logger:      logger,
		botUsername: botUsername,
		channelName: channelName,
		config:      config,
		stopCh:      make(chan struct{}),
		done:        make(chan struct{}),
	}
}

// Start launches the polling loop in a goroutine.
func (p *Poller) Start() {
	go p.loop()
}

// Stop signals the polling loop to stop and waits for it to finish.
// It is safe to call Stop multiple times.
func (p *Poller) Stop() {
	p.stopOnce.Do(func() { close(p.stopCh) })
	<-p.done
}

// loop runs the long-polling loop until Stop() is called.
func (p *Poller) loop() {
	defer close(p.done)

	var offset int
	var consecutiveErrors int

	for {
		select {
		case <-p.stopCh:
			return
		default:
		}

		updates, err := p.client.GetUpdates(p.ctx(), GetUpdatesRequest{
			Offset:         offset,
			Timeout:        p.config.PollingTimeout,
			AllowedUpdates: p.config.AllowedUpdates,
		})
		if err != nil {
			consecutiveErrors++
			p.logger.Error("polling getUpdates failed",
				"error", err,
				"consecutive_errors", consecutiveErrors,
			)

			if consecutiveErrors >= maxConsecutivePollingErrors {
				p.logger.Warn("polling paused after consecutive errors",
					"pause", errorPauseDuration,
				)
				select {
				case <-p.stopCh:
					return
				case <-time.After(errorPauseDuration):
				}
				consecutiveErrors = 0
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

// ctx returns a context that is cancelled when the poller stops.
// It creates a short-lived context for each poll cycle.
func (p *Poller) ctx() contextWrapper {
	return contextWrapper{stopCh: p.stopCh}
}

// handleUpdate processes a single update.
func (p *Poller) handleUpdate(update *Update) {
	msg, err := convertInbound(update, p.botUsername, p.channelName)
	if err != nil {
		p.logger.Debug("skipping update", "update_id", update.UpdateID, "reason", err)
		return
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

// contextWrapper adapts a stop channel to a context.Context for the HTTP client.
type contextWrapper struct {
	stopCh <-chan struct{}
}

func (c contextWrapper) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c contextWrapper) Done() <-chan struct{}       { return c.stopCh }

func (c contextWrapper) Err() error {
	select {
	case <-c.stopCh:
		return errPollerStopped
	default:
		return nil
	}
}

func (c contextWrapper) Value(any) any { return nil }

var errPollerStopped = pollerStoppedError{}

type pollerStoppedError struct{}

func (pollerStoppedError) Error() string { return "poller stopped" }
