package node

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flemzord/sclaw/internal/tool"
)

// DeviceTool exposes a device capability as a tool.Tool.
type DeviceTool struct {
	name        string
	description string
	capability  Capability
	schema      json.RawMessage
	store       *DeviceStore
	timeout     time.Duration
}

// Name implements tool.Tool.
func (t *DeviceTool) Name() string { return t.name }

// Description implements tool.Tool.
func (t *DeviceTool) Description() string { return t.description }

// Schema implements tool.Tool.
func (t *DeviceTool) Schema() json.RawMessage { return t.schema }

// Scopes implements tool.Tool.
func (t *DeviceTool) Scopes() []tool.Scope {
	return []tool.Scope{tool.ScopeNetwork}
}

// DefaultPolicy implements tool.Tool.
func (t *DeviceTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAsk
}

// Execute implements tool.Tool. It finds the first paired device with the
// required capability and sends a tool request to it.
func (t *DeviceTool) Execute(ctx context.Context, args json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	devices := t.store.ByCapability(t.capability)
	if len(devices) == 0 {
		return tool.Output{}, fmt.Errorf("%w: %s", ErrNoDeviceAvailable, t.capability)
	}

	device := devices[0]

	timeoutCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	resp, err := device.SendToolRequest(timeoutCtx, t.name, args)
	if err != nil {
		return tool.Output{}, err
	}

	return tool.Output{
		Content: resp.Content,
		IsError: resp.IsError,
	}, nil
}
