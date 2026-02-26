// Package telegram implements the Telegram Bot API channel for sclaw.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	maxRetries       = 3
	initialBackoff   = time.Second
	maxResponseBytes = 10 << 20 // 10 MiB — prevent unbounded reads from API responses.
)

// Client is a thin HTTP wrapper around the Telegram Bot API.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewClient creates a new Telegram Bot API client.
func NewClient(token, baseURL string) *Client {
	return &Client{
		token:   token,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// do sends a JSON POST request to the given Bot API method and decodes the response.
// It handles 429 rate limiting with Retry-After (max 3 retries, exponential backoff).
func do[T any](ctx context.Context, c *Client, method string, payload any) (*T, error) {
	url := fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method)

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("telegram: marshal %s request: %w", method, err)
		}
		body = bytes.NewReader(data)
	}

	backoff := initialBackoff

	for attempt := range maxRetries {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
		if err != nil {
			return nil, fmt.Errorf("telegram: create %s request: %w", method, err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			// Wrap without the raw error to avoid leaking the token-bearing URL
			// in error messages. The original error is still available via Unwrap.
			return nil, fmt.Errorf("telegram: %s request failed: %w", method, err)
		}

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("telegram: read %s response: %w", method, err)
		}

		// Handle rate limiting with retry.
		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries-1 {
			var apiResp APIResponse[json.RawMessage]
			if err := json.Unmarshal(respBody, &apiResp); err == nil && apiResp.Parameters != nil && apiResp.Parameters.RetryAfter > 0 {
				backoff = time.Duration(apiResp.Parameters.RetryAfter) * time.Second
			}

			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			backoff *= 2

			// Re-create body reader for retry.
			if payload != nil {
				data, _ := json.Marshal(payload)
				body = bytes.NewReader(data)
			}
			continue
		}

		var apiResp APIResponse[T]
		if err := json.Unmarshal(respBody, &apiResp); err != nil {
			return nil, fmt.Errorf("telegram: decode %s response: %w", method, err)
		}

		if !apiResp.OK {
			apiErr := &APIError{
				Code:        apiResp.ErrorCode,
				Description: apiResp.Description,
			}
			if apiResp.Parameters != nil {
				apiErr.RetryAfter = apiResp.Parameters.RetryAfter
			}
			return nil, apiErr
		}

		return &apiResp.Result, nil
	}

	// Unreachable under normal flow, but satisfy the compiler.
	return nil, fmt.Errorf("telegram: %s: max retries exceeded", method)
}

// GetUpdatesRequest is the request body for the getUpdates method.
type GetUpdatesRequest struct {
	Offset         int      `json:"offset,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Timeout        int      `json:"timeout,omitempty"`
	AllowedUpdates []string `json:"allowed_updates,omitempty"`
}

// SetWebhookRequest is the request body for the setWebhook method.
type SetWebhookRequest struct {
	URL            string   `json:"url"`
	SecretToken    string   `json:"secret_token,omitempty"`
	AllowedUpdates []string `json:"allowed_updates,omitempty"`
	MaxConnections int      `json:"max_connections,omitempty"`
}

// SendMessageRequest is the request body for the sendMessage method.
type SendMessageRequest struct {
	ChatID                int64  `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
	DisableNotification   bool   `json:"disable_notification,omitempty"`
	ReplyToMessageID      int    `json:"reply_to_message_id,omitempty"`
	MessageThreadID       int    `json:"message_thread_id,omitempty"`
}

// EditMessageTextRequest is the request body for the editMessageText method.
type EditMessageTextRequest struct {
	ChatID                int64  `json:"chat_id"`
	MessageID             int    `json:"message_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
}

// SendPhotoRequest is the request body for the sendPhoto method.
type SendPhotoRequest struct {
	ChatID              int64  `json:"chat_id"`
	Photo               string `json:"photo"`
	Caption             string `json:"caption,omitempty"`
	ParseMode           string `json:"parse_mode,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
	ReplyToMessageID    int    `json:"reply_to_message_id,omitempty"`
	MessageThreadID     int    `json:"message_thread_id,omitempty"`
}

// SendAudioRequest is the request body for the sendAudio method.
type SendAudioRequest struct {
	ChatID              int64  `json:"chat_id"`
	Audio               string `json:"audio"`
	Caption             string `json:"caption,omitempty"`
	ParseMode           string `json:"parse_mode,omitempty"`
	Duration            int    `json:"duration,omitempty"`
	Performer           string `json:"performer,omitempty"`
	Title               string `json:"title,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
	ReplyToMessageID    int    `json:"reply_to_message_id,omitempty"`
	MessageThreadID     int    `json:"message_thread_id,omitempty"`
}

// SendVoiceRequest is the request body for the sendVoice method.
type SendVoiceRequest struct {
	ChatID              int64  `json:"chat_id"`
	Voice               string `json:"voice"`
	Caption             string `json:"caption,omitempty"`
	ParseMode           string `json:"parse_mode,omitempty"`
	Duration            int    `json:"duration,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
	ReplyToMessageID    int    `json:"reply_to_message_id,omitempty"`
	MessageThreadID     int    `json:"message_thread_id,omitempty"`
}

// SendDocumentRequest is the request body for the sendDocument method.
type SendDocumentRequest struct {
	ChatID              int64  `json:"chat_id"`
	Document            string `json:"document"`
	Caption             string `json:"caption,omitempty"`
	ParseMode           string `json:"parse_mode,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
	ReplyToMessageID    int    `json:"reply_to_message_id,omitempty"`
	MessageThreadID     int    `json:"message_thread_id,omitempty"`
}

// SendLocationRequest is the request body for the sendLocation method.
type SendLocationRequest struct {
	ChatID              int64   `json:"chat_id"`
	Latitude            float64 `json:"latitude"`
	Longitude           float64 `json:"longitude"`
	DisableNotification bool    `json:"disable_notification,omitempty"`
	ReplyToMessageID    int     `json:"reply_to_message_id,omitempty"`
	MessageThreadID     int     `json:"message_thread_id,omitempty"`
}

// sendChatActionRequest is the request body for the sendChatAction method.
type sendChatActionRequest struct {
	ChatID int64  `json:"chat_id"`
	Action string `json:"action"`
}

// getFileRequest is the request body for the getFile method.
type getFileRequest struct {
	FileID string `json:"file_id"`
}

// GetMe returns the bot's user information.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	return do[User](ctx, c, "getMe", nil)
}

// GetUpdates fetches incoming updates using long polling.
func (c *Client) GetUpdates(ctx context.Context, req GetUpdatesRequest) ([]Update, error) {
	result, err := do[[]Update](ctx, c, "getUpdates", req)
	if err != nil {
		return nil, err
	}
	return *result, nil
}

// SetWebhook configures the webhook URL for receiving updates.
func (c *Client) SetWebhook(ctx context.Context, req SetWebhookRequest) error {
	_, err := do[bool](ctx, c, "setWebhook", req)
	return err
}

// DeleteWebhook removes the current webhook integration.
func (c *Client) DeleteWebhook(ctx context.Context) error {
	_, err := do[bool](ctx, c, "deleteWebhook", nil)
	return err
}

// SendMessage sends a text message to the specified chat.
func (c *Client) SendMessage(ctx context.Context, req SendMessageRequest) (*Message, error) {
	return do[Message](ctx, c, "sendMessage", req)
}

// EditMessageText edits the text of a previously sent message.
func (c *Client) EditMessageText(ctx context.Context, req EditMessageTextRequest) (*Message, error) {
	return do[Message](ctx, c, "editMessageText", req)
}

// SendPhoto sends a photo to the specified chat.
func (c *Client) SendPhoto(ctx context.Context, req SendPhotoRequest) (*Message, error) {
	return do[Message](ctx, c, "sendPhoto", req)
}

// SendAudio sends an audio file to the specified chat.
func (c *Client) SendAudio(ctx context.Context, req SendAudioRequest) (*Message, error) {
	return do[Message](ctx, c, "sendAudio", req)
}

// SendVoice sends a voice message to the specified chat.
func (c *Client) SendVoice(ctx context.Context, req SendVoiceRequest) (*Message, error) {
	return do[Message](ctx, c, "sendVoice", req)
}

// SendDocument sends a document to the specified chat.
func (c *Client) SendDocument(ctx context.Context, req SendDocumentRequest) (*Message, error) {
	return do[Message](ctx, c, "sendDocument", req)
}

// SendLocation sends a location to the specified chat.
func (c *Client) SendLocation(ctx context.Context, req SendLocationRequest) (*Message, error) {
	return do[Message](ctx, c, "sendLocation", req)
}

// SendChatAction sends a chat action (e.g., "typing") to the specified chat.
func (c *Client) SendChatAction(ctx context.Context, chatID int64, action string) error {
	_, err := do[bool](ctx, c, "sendChatAction", sendChatActionRequest{
		ChatID: chatID,
		Action: action,
	})
	return err
}

// GetFile retrieves basic info about a file and prepares it for downloading.
func (c *Client) GetFile(ctx context.Context, fileID string) (*File, error) {
	return do[File](ctx, c, "getFile", getFileRequest{FileID: fileID})
}

// FileURL returns the download URL for a file path returned by GetFile.
func (c *Client) FileURL(filePath string) string {
	return fmt.Sprintf("%s/file/bot%s/%s", c.baseURL, c.token, filePath)
}
