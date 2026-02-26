package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/gateway"
	"github.com/flemzord/sclaw/pkg/message"
	"gopkg.in/yaml.v3"
)

func init() {
	core.RegisterModule(&Telegram{})
}

// Compile-time interface guards.
var (
	_ channel.Channel          = (*Telegram)(nil)
	_ channel.StreamingChannel = (*Telegram)(nil)
	_ channel.TypingChannel    = (*Telegram)(nil)
	_ core.Configurable        = (*Telegram)(nil)
	_ core.Provisioner         = (*Telegram)(nil)
	_ core.Validator           = (*Telegram)(nil)
	_ core.Starter             = (*Telegram)(nil)
	_ core.Stopper             = (*Telegram)(nil)
)

// Telegram implements the Telegram Bot API channel for sclaw.
type Telegram struct {
	config    Config
	client    *Client
	logger    *slog.Logger
	allowList *channel.AllowList
	inbox     func(message.InboundMessage) error
	botUser   *User
	appCtx    *core.AppContext

	// Set during Start() depending on mode.
	poller          *Poller
	webhookReceiver *WebhookReceiver
}

// ModuleInfo implements core.Module.
func (t *Telegram) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "channel.telegram",
		New: func() core.Module { return &Telegram{} },
	}
}

// Configure implements core.Configurable.
func (t *Telegram) Configure(node *yaml.Node) error {
	if err := node.Decode(&t.config); err != nil {
		return fmt.Errorf("telegram: decode config: %w", err)
	}
	t.config.defaults()
	return nil
}

// Provision implements core.Provisioner.
func (t *Telegram) Provision(ctx *core.AppContext) error {
	t.appCtx = ctx
	t.logger = ctx.Logger
	t.client = NewClient(t.config.Token, t.config.APIURL)
	t.allowList = channel.NewAllowList(t.config.AllowUsers, t.config.AllowGroups)
	return nil
}

// Validate implements core.Validator.
func (t *Telegram) Validate() error {
	if t.config.Token == "" {
		return errors.New("telegram: token is required")
	}
	switch t.config.Mode {
	case "polling", "webhook":
	default:
		return fmt.Errorf("telegram: invalid mode %q (must be \"polling\" or \"webhook\")", t.config.Mode)
	}
	if t.config.Mode == "webhook" && t.config.WebhookURL == "" {
		return errors.New("telegram: webhook_url is required when mode is \"webhook\"")
	}
	return nil
}

// Start implements core.Starter. It validates the bot token, then starts
// either polling or webhook mode.
func (t *Telegram) Start() error {
	if t.inbox == nil {
		return errors.New("telegram: inbox not set, call SetInbox before Start")
	}

	// Validate token and get bot info.
	user, err := t.client.GetMe(context.Background())
	if err != nil {
		return fmt.Errorf("telegram: getMe failed (check token): %w", err)
	}
	t.botUser = user
	t.logger.Info("telegram bot authenticated",
		"id", user.ID,
		"username", user.Username,
	)

	channelName := string(t.ModuleInfo().ID)

	switch t.config.Mode {
	case "polling":
		t.poller = NewPoller(
			t.client, t.inbox, t.allowList, t.logger,
			user.Username, channelName, t.config,
		)
		t.poller.Start()
		t.logger.Info("telegram polling started",
			"timeout", t.config.PollingTimeout,
		)

	case "webhook":
		if t.config.WebhookSecret == "" {
			t.logger.Warn("telegram webhook running without secret_token — " +
				"consider setting webhook_secret for production deployments")
		}
		t.webhookReceiver = NewWebhookReceiver(
			t.client, t.inbox, t.allowList, t.logger,
			user.Username, channelName, t.config.WebhookSecret,
		)

		// Register webhook with the gateway's dispatcher.
		if err := t.registerWebhook(); err != nil {
			return err
		}

		// Set the webhook URL with Telegram.
		if err := t.client.SetWebhook(context.Background(), SetWebhookRequest{
			URL:            t.config.WebhookURL,
			SecretToken:    t.config.WebhookSecret,
			AllowedUpdates: t.config.AllowedUpdates,
		}); err != nil {
			return fmt.Errorf("telegram: setWebhook failed: %w", err)
		}
		t.logger.Info("telegram webhook configured",
			"url", t.config.WebhookURL,
		)
	}

	return nil
}

// registerWebhook resolves the gateway webhook dispatcher from the service
// registry and registers the WebhookReceiver as a handler.
func (t *Telegram) registerWebhook() error {
	svc, ok := t.appCtx.GetService("gateway.webhook_dispatcher")
	if !ok {
		return errors.New("telegram: gateway.webhook_dispatcher service not found (is the gateway module loaded?)")
	}

	dispatcher, ok := svc.(*gateway.WebhookDispatcher)
	if !ok {
		return errors.New("telegram: gateway.webhook_dispatcher is not a *gateway.WebhookDispatcher")
	}

	// Pass empty HMAC secret — Telegram uses its own X-Telegram-Bot-Api-Secret-Token
	// header instead of HMAC-SHA256. Validation is handled inside WebhookReceiver.HandleWebhook.
	dispatcher.Register("telegram", t.webhookReceiver, "")
	return nil
}

// Stop implements core.Stopper.
func (t *Telegram) Stop(ctx context.Context) error {
	t.logger.Info("telegram channel stopping")

	switch t.config.Mode {
	case "polling":
		if t.poller != nil {
			t.poller.Stop()
		}
	case "webhook":
		if err := t.client.DeleteWebhook(ctx); err != nil {
			t.logger.Warn("telegram: failed to delete webhook on shutdown", "error", err)
		}
	}

	return nil
}

// Send implements channel.Channel.
func (t *Telegram) Send(ctx context.Context, msg message.OutboundMessage) error {
	return t.sendOutbound(ctx, msg)
}

// SetInbox implements channel.Channel.
func (t *Telegram) SetInbox(fn func(msg message.InboundMessage) error) {
	t.inbox = fn
}

// SendTyping implements channel.TypingChannel.
func (t *Telegram) SendTyping(ctx context.Context, chat message.Chat) error {
	chatID, err := strconv.ParseInt(chat.ID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chat ID %q: %w", chat.ID, err)
	}
	return t.client.SendChatAction(ctx, chatID, "typing")
}
