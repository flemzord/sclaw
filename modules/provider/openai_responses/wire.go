package openairesponses

import "encoding/json"

// --- Client → Server events ---

// clientEvent is the top-level message sent over the WebSocket.
// The Responses API uses a flat structure where all fields are at the
// top level (not nested under a "response" key).
type clientEvent struct {
	Type            string          `json:"type"`
	Model           string          `json:"model"`
	Instructions    string          `json:"instructions,omitempty"`
	Input           []inputItem     `json:"input"`
	Tools           []wireTool      `json:"tools,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	Store           *bool           `json:"store,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

// inputItem is a polymorphic item in the conversation input.
// The Type field discriminates between "message", "function_call",
// and "function_call_output".
type inputItem struct {
	Type string `json:"type"`

	// Fields for type "message".
	Role    string             `json:"role,omitempty"`
	Content []inputContentPart `json:"content,omitempty"`

	// Fields for type "function_call".
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// Fields for type "function_call_output".
	// CallID and Output are used; CallID is shared with function_call.
	Output string `json:"output,omitempty"`
}

// inputContentPart is a content part within an input message.
type inputContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// wireTool describes a tool available to the model.
type wireTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// --- Server → Client events ---

// serverEvent is the top-level envelope received from the WebSocket.
// Only the fields we need are parsed; unknown fields are ignored.
type serverEvent struct {
	Type string `json:"type"`

	// Present on "response.output_text.delta".
	Delta string `json:"delta,omitempty"`

	// Present on "response.function_call_arguments.delta".
	// Delta is reused for this.

	// Present on "response.output_item.done".
	Item *outputItem `json:"item,omitempty"`

	// Present on "response.completed".
	Response *responseCompleted `json:"response,omitempty"`

	// Present on "error".
	Error *serverError `json:"error,omitempty"`

	// Present on "response.output_item.added" — carries the item_id
	// used by subsequent delta events.
	ItemID string `json:"item_id,omitempty"`

	// Present on events scoped to an output item.
	OutputIndex int `json:"output_index,omitempty"`
}

// outputItem represents a completed output item.
type outputItem struct {
	Type string `json:"type"` // "message" or "function_call"
	ID   string `json:"id,omitempty"`

	// For type "message".
	Role    string              `json:"role,omitempty"`
	Content []outputContentPart `json:"content,omitempty"`

	// For type "function_call".
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// outputContentPart is a content part within an output message.
type outputContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// responseCompleted is the payload of a "response.completed" event.
type responseCompleted struct {
	ID           string       `json:"id"`
	Status       string       `json:"status"` // "completed", "failed", "incomplete"
	Output       []outputItem `json:"output,omitempty"`
	Usage        *wireUsage   `json:"usage,omitempty"`
	StopReason   string       `json:"stop_reason,omitempty"`   // "stop", "max_output_tokens", "tool_use"
	FinishReason string       `json:"finish_reason,omitempty"` // alternative field name
}

// wireUsage carries token usage information.
type wireUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// serverError is the payload of an "error" event.
type serverError struct {
	Type    string `json:"type,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
