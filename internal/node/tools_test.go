package node

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/tool"
)

func TestDeviceTool_Name(t *testing.T) {
	t.Parallel()

	dt := &DeviceTool{
		name:        "node.camera.snap",
		description: "Take a photo",
		capability:  CapCamera,
		schema:      json.RawMessage(`{}`),
		store:       NewDeviceStore(),
		timeout:     30 * time.Second,
	}

	if dt.Name() != "node.camera.snap" {
		t.Errorf("Name() = %q, want %q", dt.Name(), "node.camera.snap")
	}
}

func TestDeviceTool_Scopes(t *testing.T) {
	t.Parallel()

	dt := &DeviceTool{
		name:        "node.location",
		description: "Get location",
		capability:  CapLocation,
		schema:      json.RawMessage(`{}`),
		store:       NewDeviceStore(),
		timeout:     30 * time.Second,
	}

	scopes := dt.Scopes()
	if len(scopes) != 1 {
		t.Fatalf("Scopes() returned %d scopes, want 1", len(scopes))
	}
	if scopes[0] != tool.ScopeNetwork {
		t.Errorf("Scopes()[0] = %q, want %q", scopes[0], tool.ScopeNetwork)
	}
}

func TestDeviceTool_DefaultPolicy(t *testing.T) {
	t.Parallel()

	dt := &DeviceTool{
		name:        "node.notification",
		description: "Send notification",
		capability:  CapNotification,
		schema:      json.RawMessage(`{}`),
		store:       NewDeviceStore(),
		timeout:     30 * time.Second,
	}

	if dt.DefaultPolicy() != tool.ApprovalAsk {
		t.Errorf("DefaultPolicy() = %q, want %q", dt.DefaultPolicy(), tool.ApprovalAsk)
	}
}

func TestDeviceTool_ExecuteNoDevice(t *testing.T) {
	t.Parallel()

	store := NewDeviceStore()
	dt := &DeviceTool{
		name:        "node.camera.snap",
		description: "Take a photo",
		capability:  CapCamera,
		schema:      json.RawMessage(`{}`),
		store:       store,
		timeout:     30 * time.Second,
	}

	_, err := dt.Execute(t.Context(), json.RawMessage(`{}`), tool.ExecutionEnv{})
	if err == nil {
		t.Fatal("expected error when no device is available")
	}
	if !errors.Is(err, ErrNoDeviceAvailable) {
		t.Errorf("error = %v, want %v", err, ErrNoDeviceAvailable)
	}
}
