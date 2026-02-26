package node

import (
	"encoding/json"
	"time"
)

// MessageType identifies the kind of WebSocket message in the node protocol.
type MessageType string

// Protocol message types exchanged over the WebSocket connection.
const (
	MsgPairRequest  MessageType = "pair_request"
	MsgPairResponse MessageType = "pair_response"
	MsgHeartbeat    MessageType = "heartbeat"
	MsgHeartbeatAck MessageType = "heartbeat_ack"
	MsgCapabilities MessageType = "capabilities"
	MsgToolRequest  MessageType = "tool_request"
	MsgToolResponse MessageType = "tool_response"
	MsgError        MessageType = "error"
)

// Envelope is the wire format for all WebSocket messages.
type Envelope struct {
	Type      MessageType     `json:"type"`
	ID        string          `json:"id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// Capability declares a device feature that can be exposed as a tool.
type Capability string

// Supported device capabilities.
const (
	CapCamera       Capability = "camera"
	CapScreen       Capability = "screen"
	CapLocation     Capability = "location"
	CapClipboard    Capability = "clipboard"
	CapNotification Capability = "notification"
	CapAudio        Capability = "audio"
	CapFilesystem   Capability = "filesystem"
)

// PairRequest is sent by the device to initiate pairing.
type PairRequest struct {
	Token      string `json:"token"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
}

// PairResponse is sent by the server after evaluating a pairing request.
type PairResponse struct {
	Accepted bool   `json:"accepted"`
	DeviceID string `json:"device_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// CapabilitiesDeclaration is sent by the device after successful pairing.
type CapabilitiesDeclaration struct {
	Capabilities []Capability `json:"capabilities"`
}

// HeartbeatPayload carries optional device telemetry.
type HeartbeatPayload struct {
	BatteryPct *int `json:"battery_pct,omitempty"`
}

// ToolRequest is sent to a device to invoke one of its capabilities.
type ToolRequest struct {
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolResponse is the device's reply to a ToolRequest.
type ToolResponse struct {
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
	Base64Data string `json:"base64_data,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
}
