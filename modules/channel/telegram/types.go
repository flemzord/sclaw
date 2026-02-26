package telegram

import "fmt"

// Update represents an incoming update from the Telegram Bot API.
type Update struct {
	UpdateID      int      `json:"update_id"`
	Message       *Message `json:"message,omitempty"`
	EditedMessage *Message `json:"edited_message,omitempty"`
	ChannelPost   *Message `json:"channel_post,omitempty"`
}

// Message represents a Telegram message.
type Message struct {
	MessageID       int             `json:"message_id"`
	From            *User           `json:"from,omitempty"`
	Chat            Chat            `json:"chat"`
	Date            int             `json:"date"`
	Text            string          `json:"text,omitempty"`
	Entities        []MessageEntity `json:"entities,omitempty"`
	Photo           []PhotoSize     `json:"photo,omitempty"`
	Audio           *Audio          `json:"audio,omitempty"`
	Voice           *Voice          `json:"voice,omitempty"`
	Document        *Document       `json:"document,omitempty"`
	Sticker         *Sticker        `json:"sticker,omitempty"`
	Location        *Location       `json:"location,omitempty"`
	Caption         string          `json:"caption,omitempty"`
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	ReplyToMessage  *Message        `json:"reply_to_message,omitempty"`
	MessageThreadID int             `json:"message_thread_id,omitempty"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title,omitempty"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// User represents a Telegram user or bot.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

// MessageEntity represents a special entity in a text message (e.g., hashtags, URLs, bot commands).
type MessageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	URL    string `json:"url,omitempty"`
	User   *User  `json:"user,omitempty"`
}

// PhotoSize represents one size of a photo or a file/sticker thumbnail.
type PhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int    `json:"file_size,omitempty"`
}

// Audio represents an audio file.
type Audio struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	Performer    string `json:"performer,omitempty"`
	Title        string `json:"title,omitempty"`
	FileName     string `json:"file_name,omitempty"`
	MIMEType     string `json:"mime_type,omitempty"`
	FileSize     int    `json:"file_size,omitempty"`
}

// Voice represents a voice note.
type Voice struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MIMEType     string `json:"mime_type,omitempty"`
	FileSize     int    `json:"file_size,omitempty"`
}

// Document represents a general file (not photos, audio, or voice).
type Document struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileName     string `json:"file_name,omitempty"`
	MIMEType     string `json:"mime_type,omitempty"`
	FileSize     int    `json:"file_size,omitempty"`
}

// Sticker represents a sticker.
type Sticker struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Type         string `json:"type"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	IsAnimated   bool   `json:"is_animated"`
	IsVideo      bool   `json:"is_video"`
	Emoji        string `json:"emoji,omitempty"`
	SetName      string `json:"set_name,omitempty"`
}

// Location represents a point on the map.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// File represents a file ready to be downloaded.
type File struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileSize     int    `json:"file_size,omitempty"`
	FilePath     string `json:"file_path,omitempty"`
}

// APIResponse is the generic wrapper returned by the Telegram Bot API.
type APIResponse[T any] struct {
	OK          bool                `json:"ok"`
	Result      T                   `json:"result"`
	Description string              `json:"description,omitempty"`
	ErrorCode   int                 `json:"error_code,omitempty"`
	Parameters  *ResponseParameters `json:"parameters,omitempty"`
}

// ResponseParameters contains information about why a request was unsuccessful.
type ResponseParameters struct {
	RetryAfter int `json:"retry_after,omitempty"`
}

// --- Request types ---

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

// --- Error types ---

// APIError represents an error returned by the Telegram Bot API.
type APIError struct {
	Code        int    `json:"error_code"`
	Description string `json:"description"`
	RetryAfter  int    `json:"retry_after,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("telegram: %d %s (retry after %ds)", e.Code, e.Description, e.RetryAfter)
	}
	return fmt.Sprintf("telegram: %d %s", e.Code, e.Description)
}
