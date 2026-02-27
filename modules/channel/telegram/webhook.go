package telegram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/gateway"
	"github.com/flemzord/sclaw/pkg/message"
)

// Compile-time interface guard.
var _ gateway.WebhookHandler = (*WebhookReceiver)(nil)

// WebhookReceiver processes incoming Telegram webhook payloads.
// It implements gateway.WebhookHandler.
type WebhookReceiver struct {
	client      *Client
	inbox       func(message.InboundMessage) error
	allowList   *channel.AllowList
	logger      *slog.Logger
	botUsername string
	channelName string
	secret      string
}

// NewWebhookReceiver creates a new WebhookReceiver.
func NewWebhookReceiver(client *Client, inbox func(message.InboundMessage) error, allowList *channel.AllowList, logger *slog.Logger, botUsername, channelName, secret string) *WebhookReceiver {
	return &WebhookReceiver{
		client:      client,
		inbox:       inbox,
		allowList:   allowList,
		logger:      logger,
		botUsername: botUsername,
		channelName: channelName,
		secret:      secret,
	}
}

// HandleWebhook processes a validated webhook payload from the gateway dispatcher.
// It validates the Telegram-specific secret token header, parses the update,
// checks the allow list, and pushes the message to the inbox.
func (w *WebhookReceiver) HandleWebhook(ctx context.Context, _ string, body []byte, headers http.Header) error {
	// Validate Telegram's secret token header if configured.
	if w.secret != "" {
		token := headers.Get("X-Telegram-Bot-Api-Secret-Token")
		if subtle.ConstantTimeCompare([]byte(w.secret), []byte(token)) != 1 {
			return errors.New("telegram: invalid webhook secret token")
		}
	}

	var update Update
	if err := json.Unmarshal(body, &update); err != nil {
		return errors.New("telegram: invalid update JSON: " + err.Error())
	}

	w.logger.Debug("received webhook update", "update_id", update.UpdateID)

	msg, err := convertInbound(&update, w.botUsername, w.channelName)
	if err != nil {
		w.logger.Debug("skipping webhook update", "update_id", update.UpdateID, "reason", err)
		return nil
	}

	w.logger.Debug("webhook inbound message converted",
		"update_id", update.UpdateID,
		"msg_id", msg.ID,
		"sender", msg.Sender.ID,
		"sender_name", msg.Sender.DisplayName,
		"chat_id", msg.Chat.ID,
		"chat_type", msg.Chat.Type,
		"blocks", len(msg.Blocks),
	)

	if msg.HasMedia() {
		if err := resolveMediaURLs(ctx, w.client, &msg); err != nil {
			w.logger.Warn("failed to resolve media URLs",
				"update_id", update.UpdateID, "error", err)
		}
	}

	if !w.allowList.IsAllowed(msg) {
		w.logger.Debug("webhook update denied by allow list",
			"update_id", update.UpdateID,
			"sender", msg.Sender.ID,
			"chat", msg.Chat.ID,
		)
		return nil
	}

	return w.inbox(msg)
}
